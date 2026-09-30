package forward

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// DeviceFilesDiff is one device whose collected files differ between two
// snapshots (CollectedFilesDiff.DeviceInfoFilesDiff). The device fields are
// the newer snapshot's. HasConfigChange is set only for a STATE listing: it
// says whether the device's configuration changed too.
type DeviceFilesDiff struct {
	InventoryDeviceInfo
	Files           []DiffFile `json:"files"`
	HasConfigChange *bool      `json:"hasConfigChange,omitempty"`
}

// DiffFile is one changed file. Name is Forward's stored file name in the
// newer snapshot ("<device>,<STATE>[,,<tag>][,<chunk>]<ext>"); NameA is the
// older snapshot's name when it differs; Command is set for a custom CLI file.
type DiffFile struct {
	Name    string `json:"name"`
	NameA   string `json:"nameA,omitempty"`
	Command string `json:"command,omitempty"`
}

// DeviceFileName returns the name Devices.DownloadFile takes for this file
// ("<STATE>[,<chunk>]<ext>", DeviceFile.getName on the appserver), and false
// for a name that route cannot address (a tagged or global file). Use
// NameA's form for the older side when it is set.
func (f DiffFile) DeviceFileName() (string, bool) {
	return deviceFileName(f.Name)
}

func deviceFileName(stored string) (string, bool) {
	_, rest, ok := strings.Cut(stored, ",")
	if !ok || rest == "" {
		return "", false
	}
	ext := path.Ext(rest)
	parts := strings.Split(strings.TrimSuffix(rest, ext), ",")
	switch {
	case len(parts) == 1:
		return rest, true
	case len(parts) == 4 && parts[1] == "" && parts[2] == "":
		// "<STATE>,,,<chunk>": an empty file id and no tag, then the chunk.
		return parts[0] + "," + parts[3] + ext, true
	}
	return "", false
}

// Files lists the devices whose collected files differ between two snapshots
// and which files changed. GET /api/diffs/{a}/{b}/files (DiffController.
// getFilesDiff, on primary 15398425a69 and stable 67e89c87124); A is the older
// snapshot. fileType narrows it to CONFIG, STATE or CUSTOM; empty lists all.
//
// Forward has no route that returns the diff text itself -- its UI fetches
// both versions and diffs them in the browser. Do the same: download each side
// with Devices.DownloadFile at snapshot A and B, using DeviceFileName.
func (s *DiffsService) Files(ctx context.Context, snapshotA, snapshotB, fileType string) ([]DeviceFilesDiff, *Response, error) {
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
	result := listResponse[DeviceFilesDiff]{Keys: []string{"deviceInfos"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}
