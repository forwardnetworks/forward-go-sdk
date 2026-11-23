package usecases

import (
	"context"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"os"
	"path/filepath"
)

// MergeFileIntoLatest is MergeIntoLatest for a ZIP on disk: it opens the file, refuses an empty one before any request (Forward
// answers 400 "File cannot be empty"), and names the upload after the file.
func (s *Snapshots) MergeFileIntoLatest(ctx context.Context, networkID, zipPath string, options domain.SnapshotMergeOptions) (*domain.SnapshotMergeResult, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
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
	return s.MergeIntoLatest(ctx, networkID, domain.SnapshotUploadFile{Name: filepath.Base(zipPath), Reader: f}, options)
}
