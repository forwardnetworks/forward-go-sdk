package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SnapshotExportOptions selects what Snapshots.Export writes. All zero exports
// the whole snapshot as it is.
//
//   - IncludeDevices or ExcludeDevices (device names or globs, never both)
//     export a subset. Forward also pulls in the devices related to an included
//     one.
//   - ObfuscationKey turns on obfuscation of sensitive data in the files (see
//     Forward's "Obfuscate a Snapshot"). Forward derives the replacements from
//     the key, and deobfuscating needs it, so keep it (I did not trace how
//     Forward's deobfuscation takes it). ObfuscateNames additionally replaces device
//     and other names, and requires a key. The key is a secret: this type's
//     String and GoString redact it, and no error from this package includes it.
//   - Only "CONFIG" restricts the ZIP to configuration files.
//   - Timeout bounds the whole call, including a long obfuscation before the
//     first byte and the transfer. Zero keeps the client's own timeout (60
//     seconds by default, which a large export exceeds); a positive value
//     replaces it; a negative value removes it, leaving the context as the only
//     limit.
type SnapshotExportOptions struct {
	IncludeDevices []string
	ExcludeDevices []string
	ObfuscationKey string
	ObfuscateNames bool
	Only           string
	Timeout        time.Duration
}

// String redacts the obfuscation key so an options value is safe to log.
func (o SnapshotExportOptions) String() string {
	key := ""
	if o.ObfuscationKey != "" {
		key = "<redacted>"
	}
	return fmt.Sprintf("SnapshotExportOptions{include=%d exclude=%d obfuscationKey=%q obfuscateNames=%t only=%q timeout=%s}",
		len(o.IncludeDevices), len(o.ExcludeDevices), key, o.ObfuscateNames, o.Only, o.Timeout)
}

// GoString redacts the key for %#v as well.
func (o SnapshotExportOptions) GoString() string { return o.String() }

// Export streams a snapshot's ZIP to dst and returns the bytes written, so a
// large export never sits in memory (ExportSubset returns the whole ZIP as a
// slice). POST /api/snapshots/{snapshotId}[?only=CONFIG] with a JSON body of
// includeDevices or excludeDevices, obfuscationKey and obfuscateNames
// (exportSnapshotSubset, published; VIEW_NETWORK_AND_SNAPSHOTS; the snapshot
// must have reached the STORED_FILES stage).
//
// Nothing is retried, because bytes may already be in dst: write to a
// temporary file and move it into place on success. If the response is not a
// ZIP the call returns an error naming the start of what came back, and dst
// holds that body, so discard it. A snapshot that is not ready yields the typed
// errors (ErrSnapshotNotProcessed and friends) before any body is written.
func (s *SnapshotsService) Export(ctx context.Context, snapshotID string, options SnapshotExportOptions, dst io.Writer) (int64, *Response, error) {
	if dst == nil {
		return 0, nil, errors.New("forward: a destination writer is required")
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return 0, nil, errors.New("forward: snapshot ID is required")
	}
	include, exclude := nonEmptyStrings(options.IncludeDevices), nonEmptyStrings(options.ExcludeDevices)
	if len(include) != 0 && len(exclude) != 0 {
		return 0, nil, errors.New("forward: specify either IncludeDevices or ExcludeDevices, not both")
	}
	if (len(options.IncludeDevices) != 0 && len(include) == 0) || (len(options.ExcludeDevices) != 0 && len(exclude) == 0) {
		return 0, nil, errors.New("forward: device filter has only blank device names")
	}
	if options.ObfuscateNames && options.ObfuscationKey == "" {
		return 0, nil, errors.New("forward: ObfuscateNames requires an ObfuscationKey")
	}
	path := "/api/snapshots/" + url.PathEscape(snapshotID)
	switch strings.ToUpper(strings.TrimSpace(options.Only)) {
	case "":
	case "CONFIG":
		path += "?" + url.Values{"only": []string{"CONFIG"}}.Encode()
	default:
		return 0, nil, fmt.Errorf("forward: export filter %q must be empty or CONFIG", options.Only)
	}
	body := map[string]any{}
	if len(include) != 0 {
		body["includeDevices"] = include
	}
	if len(exclude) != 0 {
		body["excludeDevices"] = exclude
	}
	if options.ObfuscationKey != "" {
		body["obfuscationKey"] = options.ObfuscationKey
		if options.ObfuscateNames {
			body["obfuscateNames"] = true
		}
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/zip")
	req = markOperation(req, "Snapshots.Export")
	sink := &zipSink{dst: dst}
	resp, err := s.client.withCallTimeout(options.Timeout).Do(req, sink)
	if err != nil {
		return sink.written, resp, err
	}
	if !sink.isZip() {
		return sink.written, resp, fmt.Errorf("forward: snapshot export did not return a ZIP (%d bytes, starting %q)", sink.written, sink.head())
	}
	return sink.written, resp, nil
}

// zipSink forwards to dst and keeps the first bytes, to tell a ZIP from the
// placeholder or error body Forward sends while a snapshot is not ready.
type zipSink struct {
	dst     io.Writer
	first   [200]byte
	nFirst  int
	written int64
}

func (z *zipSink) Write(p []byte) (int, error) {
	if z.nFirst < len(z.first) {
		z.nFirst += copy(z.first[z.nFirst:], p)
	}
	n, err := z.dst.Write(p)
	z.written += int64(n)
	return n, err
}

func (z *zipSink) isZip() bool { return z.nFirst >= 2 && z.first[0] == 'P' && z.first[1] == 'K' }

func (z *zipSink) head() string {
	head := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return '.'
		}
		return r
	}, string(z.first[:z.nFirst]))
	return head
}

// withCallTimeout returns a client that differs only in its HTTP timeout: the
// same client for zero, the given timeout for a positive value, none for a
// negative one. The copy shares credentials, transport and capability state.
func (c *Client) withCallTimeout(timeout time.Duration) *Client {
	if timeout == 0 || c == nil || c.httpClient == nil {
		return c
	}
	if timeout < 0 {
		timeout = 0
	}
	if c.httpClient.Timeout == timeout {
		return c
	}
	clone := *c
	httpClient := *c.httpClient
	httpClient.Timeout = timeout
	clone.httpClient = &httpClient
	return &clone
}
