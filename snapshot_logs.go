package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Collection logs, collection metrics and exception text: the routes the
// Forward UI uses to explain a slow or failed collection. None is in the
// published spec (preview); all are on primary 15398425a69 and stable
// 67e89c87124. The text routes stream whole files, so each takes maxBytes and
// stops there, reporting truncation rather than an error.

// SnapshotLogOptions narrows Snapshots.Logs. DeviceName is the name the device
// was requested under (Forward's requestedDeviceName; another name yields an
// empty log). Level is a log level filter such as "WARN". Backfilled reads the
// backfill log of one device, so it needs DeviceName.
type SnapshotLogOptions struct {
	DeviceName string
	Level      string
	Backfilled bool
}

// Logs writes up to maxBytes of a snapshot's collection log -- every device's,
// or one device's -- to dst. GET /api/snapshots/{id}/logs
// (SnapshotExportController.getSnapshotLogs). Forward answers 404 for a
// snapshot with no collection map, such as an import or a fork.
func (s *SnapshotsService) Logs(ctx context.Context, snapshotID string, options SnapshotLogOptions, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *Response, err error) {
	path, err := snapshotSubPath(snapshotID, "logs")
	if err != nil {
		return 0, false, nil, err
	}
	query := url.Values{}
	setString(query, "deviceName", options.DeviceName)
	setString(query, "level", options.Level)
	if options.Backfilled {
		if strings.TrimSpace(options.DeviceName) == "" {
			return 0, false, nil, errors.New("forward: a backfill log is per device; DeviceName is required")
		}
		query.Set("backfilled", "true")
	}
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	return textHead(ctx, s.client, path, "Snapshots.Logs", maxBytes, dst)
}

// CollectionLog writes up to maxBytes of a snapshot's CLI execution log (the
// commands the collector ran) to dst; an empty log writes nothing. GET
// /api/snapshots/{id}/collection-log (SnapshotExportController.getSnapshotLog).
func (s *SnapshotsService) CollectionLog(ctx context.Context, snapshotID string, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *Response, err error) {
	path, err := snapshotSubPath(snapshotID, "collection-log")
	if err != nil {
		return 0, false, nil, err
	}
	return textHead(ctx, s.client, path, "Snapshots.CollectionLog", maxBytes, dst)
}

// ExceptionsText writes up to maxBytes of a snapshot's processing exceptions
// as Forward's plain-text report (device name, then full stack trace) to dst.
// GET /api/snapshots/{id}/exceptions (SnapshotController.getExceptions); needs
// DEBUG_SNAPSHOTS. It reads the same stored exceptions as Exceptions, so when
// that is empty this is too: Forward can mark a device PARSER_EXCEPTION
// without storing an exception (SnapshotParsingHelper.determineProcessingError).
func (s *SnapshotsService) ExceptionsText(ctx context.Context, snapshotID string, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *Response, err error) {
	path, err := snapshotSubPath(snapshotID, "exceptions")
	if err != nil {
		return 0, false, nil, err
	}
	return textHead(ctx, s.client, path, "Snapshots.ExceptionsText", maxBytes, dst)
}

func textHead(ctx context.Context, c *Client, path, operation string, maxBytes int64, dst io.Writer) (int64, bool, *Response, error) {
	if maxBytes <= 0 || dst == nil {
		return 0, false, nil, errors.New("forward: a positive maxBytes and a destination writer are required")
	}
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, false, nil, err
	}
	// application/json too: Forward answers an error as JSON, and without it Spring can only say 406.
	req.Header.Set("Accept", "text/plain, application/json")
	req = markOperation(req, operation)
	head := &headWriter{dst: dst, remaining: maxBytes}
	resp, err := c.Do(req, head)
	if errors.Is(err, errHeadFull) {
		return head.written, true, resp, nil
	}
	return head.written, false, resp, err
}

// SnapshotCollectionMetrics is how a snapshot's collection went, device by
// device (SnapshotCollectionMetric). Times are epoch milliseconds; durations
// are milliseconds.
type SnapshotCollectionMetrics struct {
	SnapshotID                Identifier                `json:"snapshotId"`
	SnapshotTimeMillis        *int64                    `json:"snapshotTime,omitempty"`
	CollectionStartTimeMillis *int64                    `json:"collectionStartTime,omitempty"`
	CollectionEndTimeMillis   *int64                    `json:"collectionEndTime,omitempty"`
	Devices                   []DeviceCollectionMetrics `json:"metrics"`
}

// DeviceCollectionMetrics is one device's collection: when it started, how
// long it took, its slowest command, and Error -- the collection and
// processing errors merged (LabeledDeviceProblem; empty when none).
// JumpServer is the id of the jump server used, if any.
type DeviceCollectionMetrics struct {
	DeviceName                   string `json:"deviceName"`
	DeviceType                   string `json:"deviceType,omitempty"`
	SourceType                   string `json:"sourceType,omitempty"`
	ConnTypeDisplayName          string `json:"connTypeDisplayName,omitempty"`
	CollectionStartTimeMillis    *int64 `json:"collectionStartTime,omitempty"`
	CollectionDurationMillis     *int64 `json:"collectionDuration,omitempty"`
	SlowestCommand               string `json:"slowestCommand,omitempty"`
	SlowestCommandDurationMillis *int64 `json:"slowestCommandDuration,omitempty"`
	JumpServer                   string `json:"jumpServer,omitempty"`
	Error                        string `json:"error,omitempty"`
}

// CollectionMetrics returns a snapshot's per-device collection metrics -- the
// one source of per-device collection slowness. An empty snapshotID uses the
// latest processed snapshot. GET /api/networks/{id}/collection-metrics
// (MetricController); 404 when the snapshot has none, such as an import.
func (s *SnapshotsService) CollectionMetrics(ctx context.Context, networkID, snapshotID string) (*SnapshotCollectionMetrics, *Response, error) {
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	path += "/collection-metrics"
	query := url.Values{}
	setString(query, "snapshotId", snapshotID)
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.CollectionMetrics")
	out := new(SnapshotCollectionMetrics)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// CollectionExceptions are the exceptions the collectors hit while collecting
// a snapshot, deduplicated (DedupedCollectionExceptions). Total counts every
// distinct exception, of which Exceptions is the first page.
type CollectionExceptions struct {
	Total             int                   `json:"actualNumDedupedExceptions"`
	CollectorIDToName map[string]string     `json:"collectorIdToName,omitempty"`
	Exceptions        []CollectionException `json:"dedupedExceptions"`
}

// CollectionException is one distinct exception and where it happened.
// TotalOccurrences counts them all; Occurrences is the first page.
type CollectionException struct {
	StackTrace       string                          `json:"stackTrace"`
	CollectorVersion string                          `json:"collectorVersion,omitempty"`
	TotalOccurrences int                             `json:"actualNumOccurrences"`
	Occurrences      []CollectionExceptionOccurrence `json:"occurrences,omitempty"`
}

// CollectionExceptionOccurrence is one occurrence. DeviceName is empty when
// the exception was not tied to a device.
type CollectionExceptionOccurrence struct {
	CollectorID     string `json:"collectorId"`
	DeviceName      string `json:"deviceName,omitempty"`
	CollectorTaskID string `json:"collectorTaskId,omitempty"`
	Timestamp       string `json:"timestamp,omitempty"`
}

// CollectionExceptionOptions pages CollectionExceptions. Forward defaults
// Limit to 1000 and OccurrencesLimit to 10.
type CollectionExceptionOptions struct {
	Limit            *int32
	OccurrencesLimit *int32
}

// CollectionExceptions returns the collectors' exceptions for a snapshot. GET
// /api/collector/exceptions?snapshotId= (CollectorLogController).
func (s *SnapshotsService) CollectionExceptions(ctx context.Context, snapshotID string, options CollectionExceptionOptions) (*CollectionExceptions, *Response, error) {
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	query := url.Values{"snapshotId": []string{snapshotID}}
	if options.Limit != nil {
		query.Set("limit", strconv.FormatInt(int64(*options.Limit), 10))
	}
	if options.OccurrencesLimit != nil {
		query.Set("occurrencesLimit", strconv.FormatInt(int64(*options.OccurrencesLimit), 10))
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/collector/exceptions?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.CollectionExceptions")
	out := new(CollectionExceptions)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}
