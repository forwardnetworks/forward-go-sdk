package forward

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// ClientPackageType names a Forward client package (ClientPackageType on the
// server). The on-prem appserver serves only WINDOWS_X64, LINUX and
// UPDATES_XML from this route; the headless packages come from fwd.app.
type ClientPackageType string

const (
	ClientPackageManualZip          ClientPackageType = "MANUAL_ZIP"
	ClientPackageWindowsX64         ClientPackageType = "WINDOWS_X64"
	ClientPackageHeadlessWindowsX64 ClientPackageType = "HEADLESS_WINDOWS_X64"
	ClientPackageOSX                ClientPackageType = "OSX"
	ClientPackageLinux              ClientPackageType = "LINUX"
	ClientPackageHeadlessLinux      ClientPackageType = "HEADLESS_LINUX"
	ClientPackageUpdatesXML         ClientPackageType = "UPDATES_XML"
)

// ClientPackage describes a downloaded client package.
type ClientPackage struct {
	// FileName is from Content-Disposition, e.g. "fwd-unix-26.9.0-18.tar.gz"
	// (its version is the server's release). Empty if the server sent none.
	FileName string
	Size     int64
	// SHA256 is the lowercase hex digest of the bytes written.
	SHA256 string
}

// DownloadClientPackage is GET /api/software/client?type=: it streams the
// client package of packageType for the authenticated user into w and
// reports its name, size and digest. The client's request timeout does not
// apply -- a headless collector is ~225 MB -- so bound the download with ctx.
// Nothing is written to w unless Forward answers 2xx.
func (s *SoftwareCentralService) DownloadClientPackage(ctx context.Context, packageType ClientPackageType, w io.Writer) (*ClientPackage, *Response, error) {
	if w == nil {
		return nil, nil, errors.New("forward: download destination is nil")
	}
	packageType = ClientPackageType(strings.TrimSpace(string(packageType)))
	if packageType == "" {
		return nil, nil, errors.New("forward: client package type is required")
	}
	query := url.Values{"type": []string{string(packageType)}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/software/client?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "*/*")
	req = markOperation(req, "SoftwareCentral.DownloadClientPackage")

	clone := *s.client
	httpClient := *s.client.httpClient
	httpClient.Timeout = 0
	clone.httpClient = &httpClient

	digest := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(w, digest)}
	response, err := clone.do(req, counter, false, nil)
	if err != nil {
		return nil, response, err
	}
	out := &ClientPackage{Size: counter.n, SHA256: hex.EncodeToString(digest.Sum(nil))}
	if response != nil && response.Response != nil {
		if _, params, parseErr := mime.ParseMediaType(response.Header.Get("Content-Disposition")); parseErr == nil {
			out.FileName = params["filename"]
		}
	}
	return out, response, nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
