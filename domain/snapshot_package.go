package domain

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

// SnapshotPackageSource is one entry of sources.json, the index Forward writes at the top of a snapshot package (the ZIP that
// Snapshots.Export returns): one entry per collection source (a classic device, a controller, a cloud account, a Kubernetes cluster).
//
// Preview: the shape is from a real k8s_cni_control_plane_api snapshot on a primary 1.0.0-261118 build, not from Forward's source,
// which I could not find the writer of. Fields I did not see are not modelled; Raw keeps the entry whole. The reason to read it:
// RelatedDevicesWithoutSnapshots names devices the parser generated for a source that have no snapshot of their own and so never
// appear as devices, which is the only way to explain "processed, zero devices" (there is no REST route for it; Forward reads it
// for licensing usage).
type SnapshotPackageSource struct {
	Name                           string   `json:"name"`
	RequestedDeviceName            string   `json:"requestedDeviceName,omitempty"`
	RelatedDevicesWithoutSnapshots []string `json:"relatedDevicesWithoutSnapshots,omitempty"`
	Vendor                         string   `json:"vendor,omitempty"`
	DeviceType                     string   `json:"deviceType,omitempty"`
	// Files are the package files of the source, "<source>,<file>".
	Files  []string `json:"files,omitempty"`
	States []string `json:"states,omitempty"`
	// CreatedBy is the connection type id that created the source (k8s_cni_control_plane_api).
	CreatedBy string `json:"createdBy,omitempty"`
	// Error and CustomCommandError are Forward's collection error enums; "NONE" when clean.
	Error                   string `json:"error,omitempty"`
	CustomCommandError      string `json:"customCommandError,omitempty"`
	CollectionIP            string `json:"collectionIp,omitempty"`
	CollectionPort          int    `json:"collectionPort,omitempty"`
	CollectionInstantMillis int64  `json:"collectionInstantMillis,omitempty"`
	ClientPackageVersion    string `json:"clientPackageVersion,omitempty"`

	Raw map[string]json.RawMessage `json:"-"`
}

// SnapshotPackageSources is the decoded sources.json.
type SnapshotPackageSources struct {
	Sources []SnapshotPackageSource
}

// ParseSnapshotPackageSources decodes sources.json: {"sources":[...]}.
func ParseSnapshotPackageSources(r io.Reader) (*SnapshotPackageSources, error) {
	var wire struct {
		Sources []json.RawMessage `json:"sources"`
	}
	if err := json.NewDecoder(r).Decode(&wire); err != nil {
		return nil, fmt.Errorf("forward: decoding sources.json: %w", err)
	}
	out := &SnapshotPackageSources{}
	for _, entry := range wire.Sources {
		var source SnapshotPackageSource
		if err := json.Unmarshal(entry, &source); err != nil {
			return nil, fmt.Errorf("forward: decoding a sources.json entry: %w", err)
		}
		if err := json.Unmarshal(entry, &source.Raw); err != nil {
			return nil, fmt.Errorf("forward: decoding a sources.json entry: %w", err)
		}
		out.Sources = append(out.Sources, source)
	}
	return out, nil
}

// ReadSnapshotPackageSources opens a snapshot package ZIP and decodes its sources.json. A package without one is an error.
func ReadSnapshotPackageSources(packageZip io.ReaderAt, size int64) (*SnapshotPackageSources, error) {
	archive, err := zip.NewReader(packageZip, size)
	if err != nil {
		return nil, fmt.Errorf("forward: opening snapshot package: %w", err)
	}
	for _, file := range archive.File {
		if file.Name != "sources.json" {
			continue
		}
		f, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return ParseSnapshotPackageSources(f)
	}
	return nil, errors.New("forward: the snapshot package has no sources.json")
}

// Source returns the entry named name (case-insensitive), or nil.
func (p *SnapshotPackageSources) Source(name string) *SnapshotPackageSource {
	for i := range p.Sources {
		if strings.EqualFold(p.Sources[i].Name, name) {
			return &p.Sources[i]
		}
	}
	return nil
}

// DevicesWithoutSnapshots returns every device name any source lists as related-but-without-snapshot, sorted and de-duplicated.
func (p *SnapshotPackageSources) DevicesWithoutSnapshots() []string {
	seen := map[string]bool{}
	var out []string
	for _, source := range p.Sources {
		for _, name := range source.RelatedDevicesWithoutSnapshots {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

var (
	authHeaderLine = regexp.MustCompile(`(?i)((?:proxy-)?authorization["']?\s*[:=]\s*["']?)(.*)`)
	bearerOrBasic  = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`)
)

// RedactSnapshotLog copies a snapshot package's log text (cli-execution.log and the like) from src to dst with credentials
// masked: everything after "Authorization:" or "Proxy-Authorization:" on a line, and any "Bearer <token>" or "Basic <token>"
// elsewhere. Forward writes the Kubernetes bearer token into cli-execution.log unredacted (seen on primary 1.0.0-261118), so run
// this before a package log is stored, shown or put in a fixture. It is a mask for known shapes, not proof that no secret remains.
func RedactSnapshotLog(dst io.Writer, src io.Reader) error {
	reader := bufio.NewReader(src)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			if _, werr := io.WriteString(dst, redactLogLine(line)); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func redactLogLine(line string) string {
	body := strings.TrimRight(line, "\r\n")
	tail := line[len(body):]
	body = authHeaderLine.ReplaceAllString(body, "${1}<redacted>")
	body = bearerOrBasic.ReplaceAllString(body, "$1 <redacted>")
	return body + tail
}
