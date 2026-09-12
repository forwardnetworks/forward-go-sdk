package forward

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func gzipBytes(t *testing.T, payload string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The fetch side: the Software Central credential is sent, the body is kept
// byte-for-byte, and the digest is over the GZIP bytes as transferred -- the
// contract the on-prem `digest` and `?sha=` are compared against.
func TestCVEIndexDownloadKeepsGzipBytesAndDigestsThem(t *testing.T) {
	index := gzipBytes(t, `{"cves":["CVE-2026-0001"]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "sc-user" || pass != "sc-pass" {
			t.Errorf("BasicAuth() = %q, %q, %v", user, pass, ok)
		}
		if r.URL.Path != "/api/cve-index" || r.URL.RawQuery != "" {
			t.Errorf("hit %s?%s, want /api/cve-index", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(index)
	}))
	defer server.Close()
	c, err := NewClient(Config{BaseURL: server.URL, Username: "sc-user", Password: "sc-pass"})
	if err != nil {
		t.Fatal(err)
	}
	got, resp, err := c.CVEIndex.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("response = %#v", resp)
	}
	if !bytes.Equal(got.Gzip, index) {
		t.Fatalf("gzip bytes were altered in transit")
	}
	sum := sha256.Sum256(index)
	if got.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("SHA256 = %s, want digest of the gzip bytes %s", got.SHA256, hex.EncodeToString(sum[:]))
	}
}

// A 200 that is not gzip -- an error page, a truncated body -- is refused
// here, so it can never be PUT over a good index downstream.
func TestCVEIndexDownloadRefusesANonGzipBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html>maintenance</html>")
	}))
	defer server.Close()
	c, _ := NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p"})
	if _, _, err := c.CVEIndex.Download(context.Background()); !errors.Is(err, ErrCVEIndexNotGzip) {
		t.Fatalf("err = %v, want ErrCVEIndexNotGzip", err)
	}
}

// An unauthenticated fwd.app answers an EMPTY 401; it must still classify as
// an authentication error so an operator sees "the Software Central account",
// not "the org license".
func TestCVEIndexDownloadEmpty401IsAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	c, _ := NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p"})
	if _, _, err := c.CVEIndex.Download(context.Background()); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("err = %v, want ErrAuthentication", err)
	}
}

// The install side: metadata decodes the appserver's map (nulls for a bundled
// index), PUT carries `?sha=` over the exact bytes with the gzip content type,
// and a digest the caller claims that does not match the bytes never leaves
// the process.
func TestCVEIndexMetadataPutAndDelete(t *testing.T) {
	index := gzipBytes(t, `{"cves":[]}`)
	sum := sha256.Sum256(index)
	digest := hex.EncodeToString(sum[:])
	var putSHA, putCT string
	var putBody []byte
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("view") == "metadata":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"digest":"abc","indexCreatedAt":"2026-08-13T00:00:00Z","indexUploadedAt":null,"indexUploadedBy":null}`)
		case r.Method == http.MethodPut && r.URL.Path == "/api/cve-index":
			putSHA = r.URL.Query().Get("sha")
			putCT = r.Header.Get("Content-Type")
			putBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/cve-index":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c, _ := NewClient(Config{BaseURL: server.URL, Username: "admin", Password: "p"})
	ctx := context.Background()

	meta, _, err := c.CVEIndex.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta.Digest != "abc" || !meta.Bundled() {
		t.Fatalf("metadata = %#v, want digest abc and Bundled() (never uploaded)", meta)
	}

	if _, err := c.CVEIndex.Put(ctx, &CVEIndexDownload{Gzip: index, SHA256: "deadbeef"}); err == nil {
		t.Fatal("Put accepted a digest that does not match its bytes")
	}
	if _, err := c.CVEIndex.Put(ctx, &CVEIndexDownload{Gzip: []byte("not gzip")}); !errors.Is(err, ErrCVEIndexNotGzip) {
		t.Fatalf("Put of non-gzip: err = %v, want ErrCVEIndexNotGzip", err)
	}
	if _, err := c.CVEIndex.Put(ctx, &CVEIndexDownload{Gzip: index, SHA256: digest}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if putSHA != digest || putCT != "application/gzip" || !bytes.Equal(putBody, index) {
		t.Fatalf("PUT sha=%q ct=%q bodyEqual=%v; want sha=%s, application/gzip, identical bytes", putSHA, putCT, bytes.Equal(putBody, index), digest)
	}
	if _, err := c.CVEIndex.Delete(ctx); err != nil || !deleted {
		t.Fatalf("Delete: err=%v deleted=%v", err, deleted)
	}
}
