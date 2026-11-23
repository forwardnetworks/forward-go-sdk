package rest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// DownloadClientPackage is GET /api/software/client?type=: it streams the
// client package of packageType for the authenticated user into w and
// reports its name, size and digest. The client's request timeout does not
// apply -- a headless collector is ~225 MB -- so bound the download with ctx.
// Nothing is written to w unless Forward answers 2xx. It needs a user login: a collector's own credentials are
// answered 403 {"message":"Non-collector user required"} (seen live on fwd.app, where a user login got the install4j
// installer).
func (s *SoftwareCentral) DownloadClientPackage(ctx context.Context, packageType ports.ClientPackageType, w io.Writer) (*ports.ClientPackage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if w == nil {
		return nil, nil, errors.New("forward: download destination is nil")
	}
	packageType = ports.ClientPackageType(strings.TrimSpace(string(packageType)))
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
	out := &ports.ClientPackage{Size: counter.n, SHA256: hex.EncodeToString(digest.Sum(nil))}
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
