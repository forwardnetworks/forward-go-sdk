package domain

import (
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
