package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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

// Logs writes up to maxBytes of a snapshot's collection log -- every device's,
// or one device's -- to dst. GET /api/snapshots/{id}/logs
// (SnapshotExportController.getSnapshotLogs). Forward answers 404 for a
// snapshot with no collection map, such as an import or a fork.
func (s *Snapshots) Logs(ctx context.Context, snapshotID string, options ports.SnapshotLogOptions, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *ports.Response, err error) {
	if err := (*service)(s).failClosed(); err != nil {
		return 0, false, nil, err
	}
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
func (s *Snapshots) CollectionLog(ctx context.Context, snapshotID string, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *ports.Response, err error) {
	if err := (*service)(s).failClosed(); err != nil {
		return 0, false, nil, err
	}
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
func (s *Snapshots) ExceptionsText(ctx context.Context, snapshotID string, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *ports.Response, err error) {
	if err := (*service)(s).failClosed(); err != nil {
		return 0, false, nil, err
	}
	path, err := snapshotSubPath(snapshotID, "exceptions")
	if err != nil {
		return 0, false, nil, err
	}
	return textHead(ctx, s.client, path, "Snapshots.ExceptionsText", maxBytes, dst)
}

func textHead(ctx context.Context, c *Client, path, operation string, maxBytes int64, dst io.Writer) (int64, bool, *ports.Response, error) {
	// application/json too: Forward answers an error as JSON, and without it Spring can only say 406.
	return downloadHead(ctx, c, path, operation, "text/plain, application/json", maxBytes, dst)
}

// downloadHead streams up to maxBytes of a GET's body to dst, reporting
// truncation rather than an error.
func downloadHead(ctx context.Context, c *Client, path, operation, accept string, maxBytes int64, dst io.Writer) (int64, bool, *ports.Response, error) {
	if maxBytes <= 0 || dst == nil {
		return 0, false, nil, errors.New("forward: a positive maxBytes and a destination writer are required")
	}
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, false, nil, err
	}
	req.Header.Set("Accept", accept)
	req = markOperation(req, operation)
	head := &headWriter{dst: dst, remaining: maxBytes}
	resp, err := c.Do(req, head)
	if errors.Is(err, errHeadFull) {
		return head.written, true, resp, nil
	}
	return head.written, false, resp, err
}

// CollectionMetrics returns a snapshot's per-device collection metrics -- the
// one source of per-device collection slowness. An empty snapshotID uses the
// latest processed snapshot. GET /api/networks/{id}/collection-metrics
// (MetricController); 404 when the snapshot has none, such as an import.
func (s *Snapshots) CollectionMetrics(ctx context.Context, networkID, snapshotID string) (*ports.SnapshotCollectionMetrics, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.SnapshotCollectionMetrics)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// CollectionExceptions returns the collectors' exceptions for a snapshot. GET
// /api/collector/exceptions?snapshotId= (CollectorLogController).
func (s *Snapshots) CollectionExceptions(ctx context.Context, snapshotID string, options ports.CollectionExceptionOptions) (*ports.CollectionExceptions, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.CollectionExceptions)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}
