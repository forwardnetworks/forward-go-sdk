package forward

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"context"
)

// SnapshotMergeType says who wins for a device that is in both the uploaded ZIP and the network's latest snapshot (Forward's
// SnapshotMergeType). The zero value is NONE.
type SnapshotMergeType string

const (
	// SnapshotMergeNone refuses the merge when any device is in both: a 409 with ErrorResponse.ConflictDevices.
	SnapshotMergeNone SnapshotMergeType = "NONE"
	// SnapshotMergeReplaceExisting takes the uploaded ZIP's data for a device in both (locations and tags follow).
	SnapshotMergeReplaceExisting SnapshotMergeType = "REPLACE_EXISTING"
	// SnapshotMergePreserveExisting keeps the latest snapshot's data for a device in both.
	SnapshotMergePreserveExisting SnapshotMergeType = "PRESERVE_EXISTING"
)

// SnapshotMergeOptions controls Snapshots.MergeIntoLatest.
type SnapshotMergeOptions struct {
	// Note is the new snapshot's note. Empty sends none.
	Note string
	// MergeType defaults to NONE; any other value than the three constants is refused before a request.
	MergeType SnapshotMergeType
	// Timeout bounds the whole upload, as SnapshotUploadOptions.Timeout does (zero keeps the client's 60 seconds, negative removes it).
	Timeout time.Duration
}

// SnapshotMergeResult is the 202 a merge answers: the new snapshot, still UNPACKING, and its id from the Location header.
type SnapshotMergeResult struct {
	Snapshot *Snapshot
	// ID is the new snapshot's id (from Location /api/snapshots/{id}, else the body's).
	ID Identifier
}

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
func (s *SnapshotsService) MergeIntoLatest(ctx context.Context, networkID string, file SnapshotUploadFile, options SnapshotMergeOptions) (*SnapshotMergeResult, *Response, error) {
	mergeType := options.MergeType
	if mergeType == "" {
		mergeType = SnapshotMergeNone
	}
	switch mergeType {
	case SnapshotMergeNone, SnapshotMergeReplaceExisting, SnapshotMergePreserveExisting:
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
	go writeSnapshotMultipart(writer, multipartWriter, []SnapshotUploadFile{file})
	snapshot := new(Snapshot)
	resp, err := s.client.withCallTimeout(options.Timeout).Do(req, snapshot)
	if err != nil {
		return nil, resp, err
	}
	result := &SnapshotMergeResult{Snapshot: snapshot, ID: snapshot.ID}
	if resp != nil && resp.Response != nil {
		if location := resp.Header.Get("Location"); location != "" {
			if id := strings.TrimSuffix(location[strings.LastIndex(location, "/")+1:], "/"); id != "" {
				result.ID = Identifier(id)
			}
		}
	}
	if result.ID == "" {
		return result, resp, errors.New("forward: snapshot merge answered with no snapshot id")
	}
	return result, resp, nil
}

// MergeFileIntoLatest is MergeIntoLatest for a ZIP on disk: it opens the file, refuses an empty one before any request (Forward
// answers 400 "File cannot be empty"), and names the upload after the file.
func (s *SnapshotsService) MergeFileIntoLatest(ctx context.Context, networkID, zipPath string, options SnapshotMergeOptions) (*SnapshotMergeResult, *Response, error) {
	f, err := os.Open(zipPath)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if info.Size() == 0 {
		return nil, nil, fmt.Errorf("forward: snapshot ZIP %s is empty", zipPath)
	}
	return s.MergeIntoLatest(ctx, networkID, SnapshotUploadFile{Name: filepath.Base(zipPath), Reader: f}, options)
}
