package domain

import (
	"errors"
	"strings"
)

// CVEIndexMetadata is the on-prem appserver's description of the index it is
// currently serving (GET /api/cve-index?view=metadata).
type CVEIndexMetadata struct {
	// Digest is the sha256 (hex) of the gzipped index bytes.
	Digest string `json:"digest"`
	// IndexCreatedAt is when Forward built the index.
	IndexCreatedAt string `json:"indexCreatedAt"`
	// IndexUploadedAt/By are empty for the index bundled in the image, which
	// was never uploaded: an instance still on its bundled index reads as
	// "never updated" here, and that is the signal an installer acts on.
	IndexUploadedAt string `json:"indexUploadedAt"`
	IndexUploadedBy string `json:"indexUploadedBy"`
}

// Bundled reports whether the appserver is serving the index shipped in its
// image rather than one somebody uploaded.
func (m CVEIndexMetadata) Bundled() bool {
	return strings.TrimSpace(m.IndexUploadedAt) == ""
}

// CVEIndexDownload is one fetched index: the gzipped bytes exactly as fwd.app
// served them and their sha256, ready to compare against Metadata().Digest
// and to hand to Put.
type CVEIndexDownload struct {
	Gzip   []byte
	SHA256 string
}

// ErrCVEIndexNotGzip is returned when a 200 body is not a gzip stream. An
// error page or a truncated download that still returned 200 would otherwise
// be PUT over a good index; the check is here so no caller has to remember it.
var ErrCVEIndexNotGzip = errors.New("forward: cve index body is not gzip")
