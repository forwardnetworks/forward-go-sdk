package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
)

// The point of the reader is explaining "processed, zero devices": a source's generated-but-snapshotless devices must be readable,
// and a field this SDK does not model must survive in Raw.
func TestReadSnapshotPackageSources(t *testing.T) {
	raw, err := os.ReadFile("testdata/snapshot_package/sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string][]byte{"metadata.txt": []byte("x"), "sources.json": raw} {
		w, _ := zw.Create(name)
		_, _ = w.Write(body)
	}
	_ = zw.Close()

	got, err := domain.ReadSnapshotPackageSources(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	k := got.Source("K8S1")
	if k == nil || k.DeviceType != "K8S_CNI_CONTROL_PLANE" || k.CreatedBy != domain.ConnTypeK8sCniControlPlaneAPI || k.CollectionPort != 6443 || k.Error != "NONE" {
		t.Fatalf("source = %+v", k)
	}
	if len(k.RelatedDevicesWithoutSnapshots) != 3 || string(k.Raw["futureField"]) != `{"a": 1}` {
		t.Errorf("related=%v raw=%v", k.RelatedDevicesWithoutSnapshots, k.Raw["futureField"])
	}
	// k8s1_w1 appears under two sources: listed once, sorted.
	if d := strings.Join(got.DevicesWithoutSnapshots(), ","); d != "k8s1_cp1,k8s1_default_web-a,k8s1_w1" {
		t.Errorf("DevicesWithoutSnapshots = %s", d)
	}

	var empty bytes.Buffer
	zw = zip.NewWriter(&empty)
	_, _ = zw.Create("metadata.txt")
	_ = zw.Close()
	if _, err := domain.ReadSnapshotPackageSources(bytes.NewReader(empty.Bytes()), int64(empty.Len())); err == nil {
		t.Error("a package without sources.json must be an error")
	}
}

// Forward writes the cluster's bearer token into cli-execution.log; no token may survive, and lines without credentials must be
// untouched (including CRLF endings).
func TestRedactSnapshotLog(t *testing.T) {
	in := "GET /api/v1/nodes\r\nAuthorization: Bearer eyJhbGciOiJSUzI1NiJ9.abc.def\r\n" +
		"curl -H 'authorization: Basic dXNlcjpwYXNzd29yZA==' x\n" +
		"sent token Bearer abcdefghijklmnop in body\n" +
		"Proxy-Authorization=secretvalue\nplain line, no end"
	var out bytes.Buffer
	if err := domain.RedactSnapshotLog(&out, strings.NewReader(in)); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, secret := range []string{"eyJhbGci", "dXNlcjpw", "abcdefghijklmnop", "secretvalue"} {
		if strings.Contains(got, secret) {
			t.Errorf("%q survived redaction:\n%s", secret, got)
		}
	}
	for _, keep := range []string{"GET /api/v1/nodes\r\n", "Bearer <redacted> in body", "plain line, no end"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q was lost:\n%s", keep, got)
		}
	}
}
