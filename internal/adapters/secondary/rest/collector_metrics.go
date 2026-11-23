package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

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
func (s *Collectors) Metrics(ctx context.Context, collectorName string, start, end time.Time, samplesPerSeries int) ([]ports.CollectorMetricSample, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
		Metrics []ports.CollectorMetricSample `json:"metrics"`
	}
	resp, err := s.client.doRequired(req, &body)
	if err != nil {
		return nil, resp, err
	}
	return body.Metrics, resp, nil
}
