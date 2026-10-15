package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CollectorCPU is the collector host's CPU: Load is the system-wide recent
// load as a fraction (0 to 1; Forward's own health monitor reads idle as
// 1 - Load), not the collector process's share. A Load below 0 means "not
// measured yet" and 0 can mean the platform does not report it, so neither is
// proof of an idle host. Cores is the logical core count.
type CollectorCPU struct {
	Load  float32 `json:"cpuLoad"`
	Cores int     `json:"cpuCores"`
}

// CollectorMemory is in megabytes: the VM's total and free memory, the JVM's
// maximum heap and free heap, and the collector process's resident set.
// JVMFreeMB is free space within the heap the JVM has COMMITTED so far, not
// within the maximum, so JVMMaxMB - JVMFreeMB overstates heap in use.
type CollectorMemory struct {
	VMTotalMB    int `json:"vmMemTotalMb"`
	VMFreeMB     int `json:"vmMemFreeMb"`
	JVMMaxMB     int `json:"jvmMemMaxMb"`
	JVMFreeMB    int `json:"jvmMemFreeMb"`
	ProcessRSSMB int `json:"procRssMb"`
}

// CollectorGC is the JVM's garbage collection. TimeSeconds is a CUMULATIVE
// counter, the total GC time since the collector's JVM started (it resets when
// the collector restarts), so take differences between samples; it is not the
// GC time of one sample. Load is the fraction of the interval since the
// previous UPLOADED sample spent in GC (that interval is about 30 seconds when
// the collector is busy and up to about 10 minutes when idle).
type CollectorGC struct {
	TimeSeconds int     `json:"gcTimeSeconds"`
	Load        float32 `json:"gcLoad"`
}

// CollectorFiles is the process's file descriptors in use and its (soft)
// limit; both are 0 where the platform does not report them.
type CollectorFiles struct {
	Max  int `json:"fileDescriptorsMax"`
	Open int `json:"fileDescriptorsOpen"`
}

// CollectorDiskUsage is one volume's size and free space in megabytes.
type CollectorDiskUsage struct {
	SizeMB      int `json:"sizeMb"`
	AvailableMB int `json:"availableMb"`
}

// CollectorDisk is the collector's data volume, the OS volume and the temp volume.
type CollectorDisk struct {
	Collector CollectorDiskUsage `json:"collector"`
	OS        CollectorDiskUsage `json:"os"`
	Tmp       CollectorDiskUsage `json:"tmp"`
}

// CollectorTaskMetrics is the collector's task executor: Concurrency is the
// executor's configured size (how many tasks it runs at once), QueueSize how
// many wait, and TasksStarted and TasksFinished running totals of persistent
// tasks since the collector process started (started minus finished is the
// tasks in flight).
type CollectorTaskMetrics struct {
	Concurrency   int `json:"concurrency"`
	QueueSize     int `json:"queueSize"`
	TasksStarted  int `json:"tasksStarted"`
	TasksFinished int `json:"tasksFinished"`
}

// CollectorMetricSample is one sample the collector uploaded. The series are
// exactly these: CPU, memory, garbage collection, file descriptors, disk and
// task executor. Forward records NO network throughput, thread count or
// per-protocol open-session series.
type CollectorMetricSample struct {
	Timestamp time.Time
	CPU       CollectorCPU
	Memory    CollectorMemory
	GC        CollectorGC
	Files     CollectorFiles
	Disk      CollectorDisk
	Tasks     CollectorTaskMetrics
}

// UnmarshalJSON reads one CollectorMetrics record (timestamp, cpuMetrics,
// memoryMetrics, gcMetrics, fileMetrics, diskMetrics, taskMetrics). The
// timestamp is an ISO-8601 instant (epoch milliseconds is also accepted, purely
// defensively).
func (m *CollectorMetricSample) UnmarshalJSON(data []byte) error {
	var wire struct {
		Timestamp json.RawMessage      `json:"timestamp"`
		CPU       CollectorCPU         `json:"cpuMetrics"`
		Memory    CollectorMemory      `json:"memoryMetrics"`
		GC        CollectorGC          `json:"gcMetrics"`
		Files     CollectorFiles       `json:"fileMetrics"`
		Disk      CollectorDisk        `json:"diskMetrics"`
		Tasks     CollectorTaskMetrics `json:"taskMetrics"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	timestamp, err := instantFromJSON(wire.Timestamp)
	if err != nil {
		return fmt.Errorf("timestamp: %w", err)
	}
	*m = CollectorMetricSample{Timestamp: timestamp, CPU: wire.CPU, Memory: wire.Memory, GC: wire.GC, Files: wire.Files, Disk: wire.Disk, Tasks: wire.Tasks}
	return nil
}

// maxCollectorMetricSamples is the most samples per series Forward returns,
// whatever is asked (CollectorMetricsController.MAX_SAMPLES_PER_SERIES).
const maxCollectorMetricSamples = 5000

// Metrics reads a collector's own resource metrics over [start, end], to tell
// whether the collector itself was the bottleneck during a collection. GET
// /api/collectors/{collectorName}/metrics (CollectorMetricsController
// .getCollectorMetrics, Preview; org DEBUG_COLLECTORS, which most logins lack:
// a typed 403, errors.Is(err, ErrPermissionDenied) and MissingPermission). The
// collector is named by its NAME, not its id.
//
// samplesPerSeries is how many samples each series keeps when the window holds
// more (Forward downsamples, keeping each series' peaks and troughs, and unions
// the kept samples, so the result can exceed it); zero uses Forward's default
// of 500, and Forward caps it at 5000. It must otherwise be at least 2.
// Samples come oldest first. A collector that has uploaded nothing in the
// window returns none. An unknown collector name is a 400, not a 404.
func (s *CollectorsService) Metrics(ctx context.Context, collectorName string, start, end time.Time, samplesPerSeries int) ([]CollectorMetricSample, *Response, error) {
	if collectorName = strings.TrimSpace(collectorName); collectorName == "" {
		return nil, nil, errors.New("forward: collector name is required")
	}
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return nil, nil, errors.New("forward: a start time and a later end time are required")
	}
	if samplesPerSeries < 0 || samplesPerSeries == 1 {
		return nil, nil, errors.New("forward: samplesPerSeries must be 0 (default) or at least 2")
	}
	query := url.Values{
		"startTime": []string{start.UTC().Format(time.RFC3339Nano)},
		"endTime":   []string{end.UTC().Format(time.RFC3339Nano)},
	}
	if samplesPerSeries > 0 {
		query.Set("samplesPerSeries", strconv.Itoa(min(samplesPerSeries, maxCollectorMetricSamples)))
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/collectors/"+url.PathEscape(collectorName)+"/metrics?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.Metrics")
	var body struct {
		Metrics []CollectorMetricSample `json:"metrics"`
	}
	resp, err := s.client.doRequired(req, &body)
	if err != nil {
		return nil, resp, err
	}
	return body.Metrics, resp, nil
}
