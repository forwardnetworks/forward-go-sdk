package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// MergeIntoLatest uploads one snapshot ZIP and merges it with the network's LATEST snapshot into a NEW snapshot; it does not overwrite
// the latest one (POST /api/networks/{id}/snapshots?action=merge&mergeType=..., multipart "file"; SnapshotController.mergeAndCreateSnapshot,
// UPLOAD_SNAPSHOT; SnapshotService.mergeSnapshotZipIntoLatestSnapshot). The body is streamed, so a large ZIP is not buffered.
//
// Answers: 202 with the new snapshot (state UNPACKING; poll it as after Upload). With mergeType NONE and a device in both, a 409
// "Conflicting devices" whose error satisfies errors.Is(err, ErrSnapshotMergeConflict) and carries the sorted names in
// ErrorResponse.ConflictDevices; pick REPLACE_EXISTING or PRESERVE_EXISTING to resolve it. A 400 when synthetic objects collide (L3 VPN,
// L2 VPN, intranet nodes, WAN circuits and encryptors need unique connection names) or a name is used by a device or synthetic object in
// more than one ZIP, and a 503 when disk or backup space is short. Forward refuses an empty file; MergeFileIntoLatest checks that
// before sending, since a stream's size is unknown here. Preview: from the controller at release 26.11.0-10; not run against a live Forward.
func (s *Snapshots) MergeIntoLatest(ctx context.Context, networkID string, file ports.SnapshotUploadFile, options ports.SnapshotMergeOptions) (*ports.SnapshotMergeResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	mergeType := options.MergeType
	if mergeType == "" {
		mergeType = ports.SnapshotMergeNone
	}
	switch mergeType {
	case ports.SnapshotMergeNone, ports.SnapshotMergeReplaceExisting, ports.SnapshotMergePreserveExisting:
	default:
		return nil, nil, fmt.Errorf("forward: merge type %q is not NONE, REPLACE_EXISTING or PRESERVE_EXISTING", mergeType)
	}
	if file.Reader == nil {
		return nil, nil, errors.New("forward: snapshot ZIP reader is required")
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := snapshotsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"action": {"merge"}, "mergeType": {string(mergeType)}}
	if options.Note != "" {
		query.Set("note", options.Note)
	}
	path += "?" + query.Encode()

	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, reader)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, nil, err
	}
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	req = markOperation(req, "Snapshots.MergeIntoLatest")
	go writeSnapshotMultipart(writer, multipartWriter, []ports.SnapshotUploadFile{file})
	snapshot := new(ports.Snapshot)
	resp, err := s.client.withCallTimeout(options.Timeout).Do(req, snapshot)
	if err != nil {
		return nil, resp, err
	}
	result := &ports.SnapshotMergeResult{Snapshot: snapshot, ID: snapshot.ID}
	if resp != nil && resp.Response != nil {
		if location := resp.Header.Get("Location"); location != "" {
			if id := strings.TrimSuffix(location[strings.LastIndex(location, "/")+1:], "/"); id != "" {
				result.ID = ports.Identifier(id)
			}
		}
	}
	if result.ID == "" {
		return result, resp, errors.New("forward: snapshot merge answered with no snapshot id")
	}
	return result, resp, nil
}
