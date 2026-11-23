package rest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SnapshotsService lists, uploads, and downloads Forward snapshots.
type Snapshots service

// List returns snapshots for a network.
func (s *Snapshots) List(
	ctx context.Context,
	networkID string,
	options ports.SnapshotListOptions,
) ([]ports.Snapshot, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := snapshotsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	query := cloneValues(options.ExtraQuery)
	setString(query, "state", options.State)
	setInt32(query, "minSuccessfulDevices", options.MinSuccessfulDevices)
	setFloat64(query, "minSuccessfulDevicePct", options.MinSuccessfulDevicePercent)
	setInt32(query, "maxCollectionFailureDevices", options.MaxCollectionFailureDevices)
	setFloat64(query, "maxCollectionFailureDevicePct", options.MaxCollectionFailurePercent)
	setInt32(query, "maxParsingFailureDevices", options.MaxParsingFailureDevices)
	setFloat64(query, "maxParsingFailureDevicePct", options.MaxParsingFailurePercent)
	setInt32(query, "limit", options.Limit)
	setBool(query, "includeArchived", options.IncludeArchived)
	setBool(query, "includePredicted", options.IncludePredicted)
	setInt32(query, "maxResults", options.MaxResults)
	setBool(query, "skipUnprocessed", options.SkipUnprocessed)
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.Snapshot]{Keys: []string{"snapshots", "items"}}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
}

// Delete removes a snapshot.
func (s *Snapshots) Delete(ctx context.Context, snapshotID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, errors.New("forward: snapshot ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/snapshots/"+url.PathEscape(snapshotID), nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// ComputeAdvancedReachability asks Forward to compute advanced reachability for a snapshot -- the analysis internet-exposure and other
// security answers need, which a reprocessed or imported snapshot can be left without (AdvancedReachabilityState UNPROCESSED). It returns
// as soon as Forward accepts (204) and runs asynchronously; from 26.11 (FWD-61031) it also reruns a computation that failed, was canceled or timed out
// (it still does nothing for one that succeeded or is in progress); watch the snapshot's AdvancedReachabilityState or Snapshots.Progress. POST
// /api/snapshots/{id}?action=computeAdvancedReachability (published, computeAdvancedReachability; SnapshotController on primary 15398425a69
// and stable 67e89c87124). The handler requires the REACHABILITY stage, so a snapshot that has not finished it is ErrSnapshotNotProcessed
// (409 SNAPSHOT_UNAVAILABLE) and one whose processing failed is ErrSnapshotProcessingFailed. Compute-heavy, but it changes no data.
func (s *Snapshots) ComputeAdvancedReachability(ctx context.Context, snapshotID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, errors.New("forward: snapshot ID is required")
	}
	path := "/api/snapshots/" + url.PathEscape(snapshotID) + "?" + url.Values{"action": []string{"computeAdvancedReachability"}}.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Snapshots.ComputeAdvancedReachability")
	return s.client.Do(req, nil)
}

// Reprocess invalidates and reprocesses an existing snapshot.
//
// Preview: this action route is used by current Forward builds but is not part
// of the published snapshot contract.
func (s *Snapshots) Reprocess(ctx context.Context, snapshotID string) (*ports.SnapshotStateTransition, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	path := "/api/snapshots/" + url.PathEscape(snapshotID) + "?action=invalidate&reprocess=true"
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}
	transition := new(ports.SnapshotStateTransition)
	resp, err := s.client.Do(req, transition)
	return transition, resp, err
}

// Invalidate clears computed snapshot data without starting inline reprocess.
func (s *Snapshots) Invalidate(ctx context.Context, snapshotID string) (*ports.SnapshotStateTransition, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"?action=invalidate&reprocess=false", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.Invalidate")
	out := new(ports.SnapshotStateTransition)
	response, err := s.client.doRequired(req, out)
	if err == nil && strings.TrimSpace(out.State) == "" {
		err = errors.New("forward: snapshot invalidate returned no state")
	}
	return out, response, err
}

// ReprocessInvalidated reprocesses a snapshot that is already invalidated
// (Snapshots.Invalidate, or any other invalidation); it does not invalidate
// first, which is what Reprocess does. POST /api/snapshots/{id}?action=reprocess
// answers 202 with the state change (SnapshotController.reprocessSnapshot,
// Forward 26.11, FWD-61280; published). A snapshot that is not invalidated is a
// 400 "must be invalidated before reprocessing". Unlike invalidation, which needs
// the INVALIDATE_SNAPSHOTS permission, this needs only network view access, but an
// org can set a minimum role for reprocessing (SnapshotReprocessMinimumRole;
// verifyMinimumRoleRequirement refuses a caller below it). Not verified live: the
// status a build before 26.11 answers for the unmapped action, or the refusal's
// status and body. Reprocess works on every build.
func (s *Snapshots) ReprocessInvalidated(ctx context.Context, snapshotID string) (*ports.SnapshotStateTransition, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"?action=reprocess", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.ReprocessInvalidated")
	out := new(ports.SnapshotStateTransition)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

func (s *Snapshots) Favorite(ctx context.Context, snapshotID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPatch, path+"?action=favorite", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Snapshots.Favorite")
	return s.client.Do(req, nil)
}

// Unfavorite clears a snapshot's favorite flag (Favorite sets it).
// PATCH /api/snapshots/{snapshotId}?action=unfavorite (SnapshotController.unfavoriteSnapshot).
func (s *Snapshots) Unfavorite(ctx context.Context, snapshotID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPatch, path+"?action=unfavorite", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Snapshots.Unfavorite")
	return s.client.Do(req, nil)
}

// Upload imports one or more snapshot ZIP files. The multipart body is
// streamed, so large snapshots are not buffered in memory by the SDK.
func (s *Snapshots) Upload(
	ctx context.Context,
	networkID string,
	files []ports.SnapshotUploadFile,
	options ports.SnapshotUploadOptions,
) (*ports.Snapshot, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.client.requireCapability(ports.CapabilitySnapshotMultipartMerge); err != nil && len(files) > 1 {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := snapshotsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, errors.New("forward: at least one snapshot ZIP is required")
	}
	for _, file := range files {
		if file.Reader == nil {
			return nil, nil, errors.New("forward: snapshot ZIP reader is required")
		}
	}

	query := cloneValues(options.ExtraQuery)
	if options.Note != "" {
		query.Set("note", options.Note)
	}
	if options.Async {
		query.Set("async", "true")
	}
	if options.ExcludeFailedDevices {
		query.Set("excludeFailedDevices", "true")
	}
	if options.SkipSnapshotProcessing {
		query.Set("skipSnapshotProcessing", "true")
	}
	if len(query) != 0 {
		path += "?" + query.Encode()
	}

	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, reader)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, nil, err
	}
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())

	go writeSnapshotMultipart(writer, multipartWriter, files)
	snapshot := new(ports.Snapshot)
	resp, err := s.client.withCallTimeout(options.Timeout).Do(req, snapshot)
	if err != nil {
		return nil, resp, err
	}
	if snapshot.ID == "" {
		detail := strings.TrimSpace(snapshot.Message)
		if detail == "" {
			detail = "response contained no snapshot ID"
		}
		return snapshot, resp, fmt.Errorf("forward: snapshot upload failed: %s", detail)
	}
	if len(files) > 1 {
		s.client.observeCapability(ports.CapabilitySnapshotMultipartMerge)
	}
	return snapshot, resp, nil
}

// UploadMergeCompatibility preserves the changedemo merge contract: any HTTP
// status is decoded as a merge result, an empty body yields a zero result, and
// a non-empty malformed body is an error. Prefer Upload for new code.
func (s *Snapshots) UploadMergeCompatibility(ctx context.Context, networkID string, files []ports.SnapshotUploadFile) (*ports.Snapshot, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, errors.New("forward: at least one snapshot ZIP is required")
	}
	for _, file := range files {
		if file.Reader == nil {
			return nil, nil, errors.New("forward: snapshot ZIP reader is required")
		}
	}
	path, err := snapshotsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	path += "?async=true&skipSnapshotProcessing=true"
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, reader)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, nil, err
	}
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	req = markOperation(req, "Snapshots.UploadMergeCompatibility")
	go writeSnapshotMultipart(writer, multipartWriter, files)
	out := new(ports.Snapshot)
	response, err := s.client.doAccepted(req, out, true, func(int) bool { return true })
	return out, response, err
}

// ExportSubset exports selected devices from a snapshot as a ZIP, returned
// whole in memory. For a large snapshot, obfuscation or a whole-snapshot export
// use Export, which streams to a writer. Forward can
// temporarily reject a fresh snapshot until processing finishes; only the typed
// snapshot-not-processed error (or a successful non-ZIP placeholder response)
// is retried. Other API errors return immediately.
func (s *Snapshots) ExportSubset(
	ctx context.Context,
	snapshotID string,
	options ports.SnapshotSubsetRequest,
) ([]byte, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.client.requireCapability(ports.CapabilitySnapshotSubsetExport); err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	include := nonEmptyStrings(options.IncludeDevices)
	exclude := nonEmptyStrings(options.ExcludeDevices)
	if (len(include) == 0) == (len(exclude) == 0) {
		return nil, nil, errors.New("forward: exactly one of include or exclude devices is required")
	}
	attempts := options.Attempts
	if attempts <= 0 {
		attempts = 24
	}
	interval := options.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	payload := map[string][]string{"includeDevices": include}
	if len(exclude) != 0 {
		payload = map[string][]string{"excludeDevices": exclude}
	}
	path := "/api/snapshots/" + url.PathEscape(snapshotID)
	var lastResponse *ports.Response
	for attempt := 1; attempt <= attempts; attempt++ {
		req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, payload)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Accept", "application/zip")
		var archive bytes.Buffer
		response, err := s.client.Do(req, &archive)
		lastResponse = response
		if err == nil && bytes.HasPrefix(archive.Bytes(), []byte("PK")) {
			s.client.observeCapability(ports.CapabilitySnapshotSubsetExport)
			return archive.Bytes(), response, nil
		}
		if err != nil && !errors.Is(err, ports.ErrSnapshotNotProcessed) {
			return nil, response, err
		}
		if attempt == attempts {
			break
		}
		if err := ports.WaitInterval(ctx, interval); err != nil {
			return nil, response, err
		}
	}
	return nil, lastResponse, fmt.Errorf("%w: snapshot %s did not produce a subset ZIP after %d attempts", ports.ErrSnapshotNotProcessed, snapshotID, attempts)
}

// ExportSubsetCompatibility is the explicit changedemo retry policy: retry
// every non-ZIP response, including HTTP errors, for the configured bounded
// attempts. It is opt-in and applies only to this read-like export POST.
func (s *Snapshots) ExportSubsetCompatibility(ctx context.Context, snapshotID string, options ports.SnapshotSubsetRequest) ([]byte, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	include := nonEmptyStrings(options.IncludeDevices)
	exclude := nonEmptyStrings(options.ExcludeDevices)
	if (len(include) == 0) == (len(exclude) == 0) {
		return nil, nil, errors.New("forward: exactly one of include or exclude devices is required")
	}
	attempts := options.Attempts
	if attempts <= 0 {
		attempts = 24
	}
	interval := options.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	payload := map[string][]string{"includeDevices": include}
	if len(exclude) != 0 {
		payload = map[string][]string{"excludeDevices": exclude}
	}
	path := "/api/snapshots/" + url.PathEscape(snapshotID)
	var last *ports.Response
	for attempt := 1; attempt <= attempts; attempt++ {
		req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, payload)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Accept", "application/zip")
		req = markOperation(req, "Snapshots.ExportSubsetCompatibility")
		var archive bytes.Buffer
		response, err := s.client.doAccepted(req, &archive, true, func(int) bool { return true })
		last = response
		if err == nil && bytes.HasPrefix(archive.Bytes(), []byte("PK")) {
			return archive.Bytes(), response, nil
		}
		if err != nil {
			return nil, response, err
		}
		if attempt < attempts {
			if err := ports.WaitInterval(ctx, interval); err != nil {
				return nil, response, err
			}
		}
	}
	return nil, last, fmt.Errorf("%w: snapshot %s did not produce a subset ZIP after %d attempts", ports.ErrSnapshotNotProcessed, snapshotID, attempts)
}

// ListDocument is the operation-specific typed binding for consumers whose
// public contract intentionally preserves Forward's JSON envelope.
func (s *Snapshots) ListDocument(ctx context.Context, networkID string, options ports.SnapshotListOptions) (ports.JSONDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := snapshotsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{}
	if options.Limit != nil {
		query.Set("limit", strconv.FormatInt(int64(*options.Limit), 10))
	}
	if options.MaxResults != nil {
		query.Set("maxResults", strconv.FormatInt(int64(*options.MaxResults), 10))
	}
	if options.State != "" {
		query.Set("state", options.State)
	}
	setBool(query, "includePredicted", options.IncludePredicted)
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.ListDocument")
	var out ports.JSONDocument
	response, err := s.client.doRequired(req, &out)
	return out, response, err
}

// Download writes an exported snapshot ZIP to dst.
func (s *Snapshots) Download(ctx context.Context, snapshotID string, dst io.Writer) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" || dst == nil {
		return nil, errors.New("forward: snapshot ID and destination writer are required")
	}
	path := "/api/snapshots/" + url.PathEscape(snapshotID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/zip")
	return s.client.Do(req, dst)
}

func snapshotsPath(networkID string) (string, error) {
	networkID = strings.TrimSpace(networkID)
	if networkID == "" {
		return "", errors.New("forward: network ID is required")
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/snapshots", nil
}

func writeSnapshotMultipart(pipe *io.PipeWriter, writer *multipart.Writer, files []ports.SnapshotUploadFile) {
	var writeErr error
	for i, file := range files {
		name := strings.TrimSpace(file.Name)
		if name == "" {
			name = fmt.Sprintf("snapshot-%d.zip", i+1)
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(name)))
		header.Set("Content-Type", "application/zip")
		part, err := writer.CreatePart(header)
		if err != nil {
			writeErr = err
			break
		}
		if _, err := io.Copy(part, file.Reader); err != nil {
			writeErr = err
			break
		}
	}
	if closeErr := writer.Close(); writeErr == nil {
		writeErr = closeErr
	}
	_ = pipe.CloseWithError(writeErr)
}

func escapeQuotes(value string) string {
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, "\"", "_")
	value = strings.ReplaceAll(value, "\r", "_")
	return strings.ReplaceAll(value, "\n", "_")
}

func cloneValues(source url.Values) url.Values {
	result := url.Values{}
	for key, values := range source {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func setString(values url.Values, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		values.Set(key, value)
	}
}

func setInt32(values url.Values, key string, value *int32) {
	if value != nil {
		values.Set(key, strconv.FormatInt(int64(*value), 10))
	}
}

func setFloat64(values url.Values, key string, value *float64) {
	if value != nil {
		values.Set(key, strconv.FormatFloat(*value, 'g', -1, 64))
	}
}

func setBool(values url.Values, key string, value *bool) {
	if value != nil {
		values.Set(key, strconv.FormatBool(*value))
	}
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

// SetNote records a note against a snapshot.
//
// Collection takes no note, so a caller that wants one sets it afterwards.
// The updated snapshot is returned.
func (s *Snapshots) SetNote(ctx context.Context, snapshotID, note string) (*ports.Snapshot, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	path := "/api/snapshots/" + url.PathEscape(snapshotID)
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, struct {
		Note string `json:"note"`
	}{Note: note})
	if err != nil {
		return nil, nil, err
	}
	snapshot := new(ports.Snapshot)
	resp, err := s.client.Do(req, snapshot)
	return snapshot, resp, err
}
