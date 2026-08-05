package forward

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
type SnapshotsService service

// Snapshot describes a Forward network snapshot.
type Snapshot struct {
	ID                        Identifier `json:"id"`
	State                     string     `json:"state,omitempty"`
	AdvancedReachabilityState string     `json:"advancedReachabilityState,omitempty"`
	CreatedAt                 string     `json:"createdAt,omitempty"`
	ProcessedAt               string     `json:"processedAt,omitempty"`
	RestoredAt                string     `json:"restoredAt,omitempty"`
	FavoritedAt               string     `json:"favoritedAt,omitempty"`
	FavoritedBy               string     `json:"favoritedBy,omitempty"`
	FavoritedByUserID         Identifier `json:"favoritedByUserId,omitempty"`
	ParentSnapshotID          Identifier `json:"parentSnapshotId,omitempty"`
	ChangeSetID               Identifier `json:"changeSetId,omitempty"`
	ProcessingTrigger         string     `json:"processingTrigger,omitempty"`
	Note                      string     `json:"note,omitempty"`
	IsDraft                   bool       `json:"isDraft,omitempty"`
	IsPredicted               bool       `json:"isPredicted,omitempty"`
	TotalDevices              int        `json:"totalDevices,omitempty"`
	Message                   string     `json:"message,omitempty"`
}

// SnapshotStateTransition is returned by snapshot invalidation/reprocessing.
type SnapshotStateTransition struct {
	PreviousState string `json:"previousState"`
	State         string `json:"state"`
}

// SnapshotListOptions contains published filters plus preview compatibility
// options used by Forward builds before they enter the OpenAPI description.
type SnapshotListOptions struct {
	State                       string
	MinSuccessfulDevices        *int32
	MinSuccessfulDevicePercent  *float64
	MaxCollectionFailureDevices *int32
	MaxCollectionFailurePercent *float64
	MaxParsingFailureDevices    *int32
	MaxParsingFailurePercent    *float64
	Limit                       *int32
	IncludeArchived             *bool

	// Preview: MaxResults and SkipUnprocessed are accepted by some Forward
	// versions but are not in the published snapshot OpenAPI contract.
	MaxResults      *int32
	SkipUnprocessed *bool

	// ExtraQuery carries version-specific flags that the typed SDK does not yet
	// know. Typed fields above take precedence over duplicate keys.
	ExtraQuery url.Values
}

// SnapshotUploadFile is one snapshot ZIP to import. Multiple files are merged
// by Forward and must not contain overlapping devices.
type SnapshotUploadFile struct {
	Name   string
	Reader io.Reader
}

// SnapshotUploadOptions controls snapshot import behavior.
type SnapshotUploadOptions struct {
	Note string

	// Preview: Async returns after acceptance rather than after processing.
	Async bool
	// Preview: ExcludeFailedDevices removes failed devices while merging files.
	ExcludeFailedDevices bool
	// Preview: SkipSnapshotProcessing stores the upload without starting
	// processing. The caller must explicitly reprocess it later.
	SkipSnapshotProcessing bool

	// ExtraQuery carries additional version-specific upload flags. Typed fields
	// above take precedence over duplicate keys.
	ExtraQuery url.Values
}

// SnapshotSubsetRequest selects one side of a disjoint snapshot merge. Exactly
// one of IncludeDevices or ExcludeDevices must be non-empty.
type SnapshotSubsetRequest struct {
	IncludeDevices []string
	ExcludeDevices []string
	PollInterval   time.Duration
	Attempts       int
}

var ErrNoSnapshots = errors.New("forward: network has no snapshots")

// List returns snapshots for a network.
func (s *SnapshotsService) List(
	ctx context.Context,
	networkID string,
	options SnapshotListOptions,
) ([]Snapshot, *Response, error) {
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
	setInt32(query, "maxResults", options.MaxResults)
	setBool(query, "skipUnprocessed", options.SkipUnprocessed)
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[Snapshot]{Keys: []string{"snapshots", "items"}}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
}

// Get returns snapshot metadata using the preview network-scoped route used by
// current Terraform and orchestration clients.
//
// Preview: this metadata route is not in the published OpenAPI description.
func (s *SnapshotsService) Get(ctx context.Context, networkID, snapshotID string) (*Snapshot, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := snapshotsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	path += "/" + url.PathEscape(snapshotID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	snapshot := new(Snapshot)
	resp, err := s.client.Do(req, snapshot)
	return snapshot, resp, err
}

// LatestProcessed returns the most recent processed snapshot for a network.
func (s *SnapshotsService) LatestProcessed(ctx context.Context, networkID string) (*Snapshot, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := snapshotsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/latestProcessed", nil)
	if err != nil {
		return nil, nil, err
	}
	snapshot := new(Snapshot)
	resp, err := s.client.Do(req, snapshot)
	return snapshot, resp, err
}

// Delete removes a snapshot.
func (s *SnapshotsService) Delete(ctx context.Context, snapshotID string) (*Response, error) {
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, errors.New("forward: snapshot ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/snapshots/"+url.PathEscape(snapshotID), nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// Reprocess invalidates and reprocesses an existing snapshot.
//
// Preview: this action route is used by current Forward builds but is not part
// of the published snapshot contract.
func (s *SnapshotsService) Reprocess(ctx context.Context, snapshotID string) (*SnapshotStateTransition, *Response, error) {
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	path := "/api/snapshots/" + url.PathEscape(snapshotID) + "?action=invalidate&reprocess=true"
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}
	transition := new(SnapshotStateTransition)
	resp, err := s.client.Do(req, transition)
	return transition, resp, err
}

// Invalidate clears computed snapshot data without starting inline reprocess.
func (s *SnapshotsService) Invalidate(ctx context.Context, snapshotID string) (*SnapshotStateTransition, *Response, error) {
	path, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"?action=invalidate&reprocess=false", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.Invalidate")
	out := new(SnapshotStateTransition)
	response, err := s.client.doRequired(req, out)
	if err == nil && strings.TrimSpace(out.State) == "" {
		err = errors.New("forward: snapshot invalidate returned no state")
	}
	return out, response, err
}

func (s *SnapshotsService) Favorite(ctx context.Context, snapshotID string) (*Response, error) {
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

// Upload imports one or more snapshot ZIP files. The multipart body is
// streamed, so large snapshots are not buffered in memory by the SDK.
func (s *SnapshotsService) Upload(
	ctx context.Context,
	networkID string,
	files []SnapshotUploadFile,
	options SnapshotUploadOptions,
) (*Snapshot, *Response, error) {
	if err := s.client.requireCapability(CapabilitySnapshotMultipartMerge); err != nil && len(files) > 1 {
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
	snapshot := new(Snapshot)
	resp, err := s.client.Do(req, snapshot)
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
		s.client.observeCapability(CapabilitySnapshotMultipartMerge)
	}
	return snapshot, resp, nil
}

// UploadMergeCompatibility preserves the changedemo merge contract: any HTTP
// status is decoded as a merge result, an empty body yields a zero result, and
// a non-empty malformed body is an error. Prefer Upload for new code.
func (s *SnapshotsService) UploadMergeCompatibility(ctx context.Context, networkID string, files []SnapshotUploadFile) (*Snapshot, *Response, error) {
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
	out := new(Snapshot)
	response, err := s.client.doAccepted(req, out, true, func(int) bool { return true })
	return out, response, err
}

// ExportSubset exports selected devices from a snapshot as a ZIP. Forward can
// temporarily reject a fresh snapshot until processing finishes; only the typed
// snapshot-not-processed error (or a successful non-ZIP placeholder response)
// is retried. Other API errors return immediately.
func (s *SnapshotsService) ExportSubset(
	ctx context.Context,
	snapshotID string,
	options SnapshotSubsetRequest,
) ([]byte, *Response, error) {
	if err := s.client.requireCapability(CapabilitySnapshotSubsetExport); err != nil {
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
	var lastResponse *Response
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
			s.client.observeCapability(CapabilitySnapshotSubsetExport)
			return archive.Bytes(), response, nil
		}
		if err != nil && !errors.Is(err, ErrSnapshotNotProcessed) {
			return nil, response, err
		}
		if attempt == attempts {
			break
		}
		if err := waitOperationInterval(ctx, interval); err != nil {
			return nil, response, err
		}
	}
	return nil, lastResponse, fmt.Errorf("%w: snapshot %s did not produce a subset ZIP after %d attempts", ErrSnapshotNotProcessed, snapshotID, attempts)
}

// ExportSubsetCompatibility is the explicit changedemo retry policy: retry
// every non-ZIP response, including HTTP errors, for the configured bounded
// attempts. It is opt-in and applies only to this read-like export POST.
func (s *SnapshotsService) ExportSubsetCompatibility(ctx context.Context, snapshotID string, options SnapshotSubsetRequest) ([]byte, *Response, error) {
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
	var last *Response
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
			if err := waitOperationInterval(ctx, interval); err != nil {
				return nil, response, err
			}
		}
	}
	return nil, last, fmt.Errorf("%w: snapshot %s did not produce a subset ZIP after %d attempts", ErrSnapshotNotProcessed, snapshotID, attempts)
}

// ListDocument is the operation-specific typed binding for consumers whose
// public contract intentionally preserves Forward's JSON envelope.
func (s *SnapshotsService) ListDocument(ctx context.Context, networkID string, options SnapshotListOptions) (JSONDocument, *Response, error) {
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
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.ListDocument")
	var out JSONDocument
	response, err := s.client.doRequired(req, &out)
	return out, response, err
}

// ResolveID implements the consumer-safe "latest" rule: prefer a processed,
// real collected snapshot; then any processed snapshot; then the newest row.
// A concrete ID is returned unchanged without a request.
func (s *SnapshotsService) ResolveID(ctx context.Context, networkID, which string) (Identifier, *Response, error) {
	which = strings.TrimSpace(which)
	if which != "" && !strings.EqualFold(which, "latest") {
		return Identifier(which), nil, nil
	}
	limit := int32(50)
	snapshots, response, err := s.List(ctx, networkID, SnapshotListOptions{Limit: &limit})
	if err != nil {
		return "", response, err
	}
	for _, snapshot := range snapshots {
		if strings.EqualFold(snapshot.State, "PROCESSED") && !snapshot.predicted() {
			return snapshot.ID, response, nil
		}
	}
	for _, snapshot := range snapshots {
		if strings.EqualFold(snapshot.State, "PROCESSED") {
			return snapshot.ID, response, nil
		}
	}
	if len(snapshots) != 0 {
		return snapshots[0].ID, response, nil
	}
	return "", response, ErrNoSnapshots
}

func (s Snapshot) predicted() bool {
	return s.IsPredicted || s.ParentSnapshotID != "" || s.ChangeSetID != ""
}

// Download writes an exported snapshot ZIP to dst.
func (s *SnapshotsService) Download(ctx context.Context, snapshotID string, dst io.Writer) (*Response, error) {
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

func writeSnapshotMultipart(pipe *io.PipeWriter, writer *multipart.Writer, files []SnapshotUploadFile) {
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
