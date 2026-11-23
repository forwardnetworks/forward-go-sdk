package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// Files lists the devices whose collected files differ between two snapshots
// and which files changed. GET /api/diffs/{a}/{b}/files (DiffController.
// getFilesDiff, on primary 15398425a69 and stable 67e89c87124); A is the older
// snapshot. fileType narrows it to CONFIG, STATE or CUSTOM; empty lists all.
//
// Forward has no route that returns the diff text itself -- its UI fetches
// both versions and diffs them in the browser. Do the same: download each side
// with Devices.DownloadFile at snapshot A and B, using DeviceFileName.
func (s *Diffs) Files(ctx context.Context, snapshotA, snapshotB, fileType string) ([]ports.DeviceFilesDiff, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	p, err := diffsPath(snapshotA, snapshotB, "files")
	if err != nil {
		return nil, nil, err
	}
	if fileType = strings.TrimSpace(fileType); fileType != "" {
		p += "?" + url.Values{"type": []string{fileType}}.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, p, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Diffs.Files")
	result := listResponse[ports.DeviceFilesDiff]{Keys: []string{"deviceInfos"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}
