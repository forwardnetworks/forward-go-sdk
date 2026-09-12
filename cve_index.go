package forward

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// CVEIndexService is the appliance's CVE index -- the vulnerability catalogue
// device-vulnerability matching reads -- and the same route on fwd.app where
// Forward publishes it. One route, `/api/cve-index`, two sides:
//
//   - On fwd.app (a Software Central account, CveIndexCloudController under
//     @Profile(SAAS)): GET returns the gzipped index. Construct the client
//     against https://fwd.app with the Software Central credential, exactly
//     as SoftwareCentralService documents.
//   - On an on-prem appserver: GET ?view=metadata answers the digest and
//     upload provenance of the index it is serving; PUT ?sha=<sha256> installs
//     a gzipped index; DELETE reverts to the index bundled in the image. PUT
//     and DELETE require a Forward admin; metadata requires the
//     SOFTWARE_CENTRAL org property or a Forward admin.
//
// THE INDEX IS INSTANCE-WIDE. putCveIndex takes no orgId: every org on an
// appserver reads the same index, so an installer uploads once per instance,
// never per org. (Only Forward's OWN hourly updater fetches per org, because
// the entitlement it presents to fwd.app hangs off the org's license; that
// updater answers 403 "License authentication failed" on self-hosted
// deployments whose orgs hold no such license, which is why this surface
// exists.)
//
// THE DIGEST IS OF THE GZIP BYTES. `digest` in the metadata and `sha` on the
// PUT are sha256 over the compressed body as transferred -- verified against
// a real upload -- so a caller can compare what it fetched to what an
// instance serves without decompressing either.
type CVEIndexService service

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

// maxCVEIndexBytes bounds a download. The real index is tens of megabytes;
// this refuses a runaway body without being a limit anyone will reach.
const maxCVEIndexBytes = 1 << 30

// Download fetches the current gzipped CVE index from fwd.app with the
// Software Central credential the client was built with. The returned bytes
// are validated as gzip and digested; nothing is decompressed beyond the
// header check.
func (s *CVEIndexService) Download(ctx context.Context) (*CVEIndexDownload, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/cve-index", nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/gzip, */*")
	req = markOperation(req, "CVEIndex.Download")
	var buf bytes.Buffer
	response, err := s.client.doRequired(req, &limitedWriter{w: &buf, remaining: maxCVEIndexBytes})
	if err != nil {
		return nil, response, err
	}
	body := buf.Bytes()
	if err := gzipHeaderOK(body); err != nil {
		return nil, response, err
	}
	sum := sha256.Sum256(body)
	return &CVEIndexDownload{Gzip: body, SHA256: hex.EncodeToString(sum[:])}, response, nil
}

// Metadata reads what an on-prem appserver is serving.
func (s *CVEIndexService) Metadata(ctx context.Context) (*CVEIndexMetadata, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/cve-index?view=metadata", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CVEIndex.Metadata")
	var out CVEIndexMetadata
	response, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, response, err
	}
	return &out, response, nil
}

// Put installs a gzipped index on an on-prem appserver. The sha256 is sent as
// `?sha=` so the server validates the body it received against the digest the
// caller computed; a mismatch is a 4xx, never a silently corrupt index. A 200
// says the request was ACCEPTED -- read Metadata back to prove the instance
// now serves that digest, which is what an installer's verdict should rest on.
func (s *CVEIndexService) Put(ctx context.Context, index *CVEIndexDownload) (*Response, error) {
	if index == nil || len(index.Gzip) == 0 {
		return nil, errors.New("forward: cve index is empty")
	}
	if err := gzipHeaderOK(index.Gzip); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(index.Gzip)
	digest := hex.EncodeToString(sum[:])
	if want := strings.ToLower(strings.TrimSpace(index.SHA256)); want != "" && want != digest {
		return nil, fmt.Errorf("forward: cve index sha256 %s does not match its bytes (%s)", want, digest)
	}
	path := "/api/cve-index?sha=" + url.QueryEscape(digest)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, bytes.NewReader(index.Gzip))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/gzip")
	req.ContentLength = int64(len(index.Gzip))
	req = markOperation(req, "CVEIndex.Put")
	return s.client.doAccepted(req, nil, true, nil)
}

// Delete reverts an on-prem appserver to the index bundled in its image.
func (s *CVEIndexService) Delete(ctx context.Context) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/cve-index", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "CVEIndex.Delete")
	return s.client.doAccepted(req, nil, true, nil)
}

// gzipHeaderOK proves the bytes open as a gzip stream. Only the header is
// read: the index is large and the digest, not the payload, is the contract.
func gzipHeaderOK(body []byte) error {
	if len(body) == 0 {
		return ErrCVEIndexNotGzip
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCVEIndexNotGzip, err)
	}
	_ = zr.Close()
	return nil
}

// limitedWriter refuses to grow past its budget, so a runaway response body
// cannot exhaust memory through the io.Writer path in Client.do.
type limitedWriter struct {
	w         io.Writer
	remaining int64
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.remaining {
		return 0, fmt.Errorf("forward: response body exceeds %d bytes", maxCVEIndexBytes)
	}
	n, err := l.w.Write(p)
	l.remaining -= int64(n)
	return n, err
}
