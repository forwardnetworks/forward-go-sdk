package forward

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A realistic fwd.app catalog: one INSTALLATION artifact carrying an OVA and
// a TAR_GZ version, one APP_UPGRADE artifact with a base-version window.
// uploadedAt is a JSON NUMBER (epoch millis), which is the shape that bites
// a decoder written against a string.
const softwareCentralCatalogFixture = `[
  {
    "id": 101,
    "name": "forward-vm",
    "deploymentTypes": ["VMWARE_HYPERVISOR", "KVM_HYPERVISOR"],
    "workflow": "INSTALLATION",
    "versions": [
      {"id": 5001, "version": "26.30.0", "fileType": "OVA", "sizeInBytes": 3221225472,
       "sha256": "aaaa", "uploadedBy": "release@forwardnetworks.com", "uploadedAt": 1757500000000},
      {"id": 5002, "version": "26.30.0", "fileType": "TAR_GZ", "sizeInBytes": 2147483648,
       "sha256": "bbbb", "uploadedBy": "release@forwardnetworks.com", "uploadedAt": 1757500000001}
    ]
  },
  {
    "id": "202",
    "name": "forward-app-upgrade",
    "deploymentTypes": ["VMWARE_HYPERVISOR"],
    "workflow": "APP_UPGRADE",
    "versions": [
      {"id": "6001", "baseVersion": "26.29", "minBaseVersion": "26.20.0", "maxBaseVersion": "26.29.9",
       "version": "26.30.0", "fileType": "PACKAGE", "sizeInBytes": 1048576,
       "sha256": "cccc", "uploadedBy": "release@forwardnetworks.com", "uploadedAt": 1757400000000}
    ]
  }
]`

// THE CREDENTIAL IS SENT, AND THE FILTER IS SENT. fwd.app answers an
// unauthenticated call with an empty 401, so "the header was there" has to
// be proven by the server the test controls, not inferred from a 200.
func TestSoftwareCentralListSendsBasicAuthAndTypeFilter(t *testing.T) {
	var gotPath, gotType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "sc-user@example.com" || pass != "sc-pass" {
			t.Errorf("BasicAuth() = %q, %q, %v; want the configured Software Central credential", user, pass, ok)
		}
		gotPath, gotType = r.URL.Path, r.URL.Query().Get("type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, softwareCentralCatalogFixture)
	}))
	defer server.Close()
	c, err := NewClient(Config{BaseURL: server.URL, Username: "sc-user@example.com", Password: "sc-pass"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	artifacts, resp, err := c.SoftwareCentral.ListDeploymentArtifacts(context.Background(), DeploymentTypeVMwareHypervisor)
	if err != nil {
		t.Fatalf("ListDeploymentArtifacts: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("response = %#v", resp)
	}
	if gotPath != "/api/deployment-artifacts" || gotType != "VMWARE_HYPERVISOR" {
		t.Fatalf("hit %s?type=%s, want /api/deployment-artifacts?type=VMWARE_HYPERVISOR", gotPath, gotType)
	}
	if len(artifacts) != 2 {
		t.Fatalf("got %d artifacts, want 2", len(artifacts))
	}

	install := artifacts[0]
	if install.ID != "101" || install.Name != "forward-vm" || install.Workflow != DeploymentWorkflowInstallation {
		t.Errorf("artifact[0] = %#v", install)
	}
	if len(install.DeploymentTypes) != 2 || install.DeploymentTypes[0] != DeploymentTypeVMwareHypervisor || install.DeploymentTypes[1] != DeploymentTypeKVMHypervisor {
		t.Errorf("artifact[0].DeploymentTypes = %v", install.DeploymentTypes)
	}
	if len(install.Versions) != 2 {
		t.Fatalf("artifact[0] has %d versions, want 2", len(install.Versions))
	}
	ova := install.Versions[0]
	if ova.ID != "5001" || ova.Version != "26.30.0" || ova.FileType != DeploymentArtifactFileTypeOVA ||
		ova.SizeInBytes != 3221225472 || ova.SHA256 != "aaaa" || ova.UploadedBy != "release@forwardnetworks.com" {
		t.Errorf("ova version = %#v", ova)
	}
	if ova.UploadedAt != 1757500000000 {
		t.Errorf("ova.UploadedAt = %d, want 1757500000000 (JSON number, epoch millis)", ova.UploadedAt)
	}
	if got := ova.UploadedTime().UnixMilli(); got != 1757500000000 {
		t.Errorf("ova.UploadedTime() = %d ms", got)
	}
	if got := install.FileName(ova); got != "forward-vm-26.30.0.ova" {
		t.Errorf("FileName(ova) = %q", got)
	}
	if got := install.FileName(install.Versions[1]); got != "forward-vm-26.30.0.tar.gz" {
		t.Errorf("FileName(tar.gz) = %q", got)
	}

	upgrade := artifacts[1]
	if upgrade.ID != "202" || upgrade.Workflow != DeploymentWorkflowAppUpgrade || len(upgrade.Versions) != 1 {
		t.Fatalf("artifact[1] = %#v", upgrade)
	}
	pkg := upgrade.Versions[0]
	if pkg.BaseVersion != "26.29" || pkg.MinBaseVersion != "26.20.0" || pkg.MaxBaseVersion != "26.29.9" || pkg.FileType != DeploymentArtifactFileTypePackage {
		t.Errorf("upgrade version = %#v", pkg)
	}
	if got := upgrade.FileName(pkg); got != "forward-app-upgrade-26.29-26.30.0.package" {
		t.Errorf("FileName(package) = %q, want the base version spliced in", got)
	}
}

// No filter means no `type` parameter at all -- not `type=`.
func TestSoftwareCentralListWithoutFilterSendsNoTypeParameter(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `[]`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	if _, _, err := c.SoftwareCentral.ListDeploymentArtifacts(context.Background(), ""); err != nil {
		t.Fatalf("ListDeploymentArtifacts: %v", err)
	}
	if gotQuery != "" {
		t.Fatalf("query = %q, want none", gotQuery)
	}
}

// fwd.app's two refusals. Unauthenticated is a 401 with content-length: 0
// (verified with curl against fwd.app on 2026-09-11) -- there is no message
// to classify by, so the classification must come from the status alone.
// Unentitled is a 403 with a JSON message naming the org; that sentence is
// what an operator needs to read, so it must survive to Error().
func TestSoftwareCentralRefusals(t *testing.T) {
	t.Run("401 empty body is ErrAuthentication and names the status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()
		c := newTestClient(t, server.URL)
		_, _, err := c.SoftwareCentral.ListDeploymentArtifacts(context.Background(), "")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !errors.Is(err, ErrAuthentication) {
			t.Errorf("errors.Is(err, ErrAuthentication) = false; err = %v", err)
		}
		if !IsErrorKind(err, ErrorKindAuthentication) {
			t.Errorf("IsErrorKind(err, ErrorKindAuthentication) = false; err = %v", err)
		}
		if !strings.Contains(err.Error(), "401") {
			t.Errorf("err = %q, want the status named", err.Error())
		}
	})
	t.Run("403 not enabled carries the server sentence verbatim", func(t *testing.T) {
		const sentence = "SOFTWARE_CENTRAL is not enabled for organization 123"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"`+sentence+`"}`)
		}))
		defer server.Close()
		c := newTestClient(t, server.URL)
		_, _, err := c.SoftwareCentral.ListDeploymentArtifacts(context.Background(), DeploymentTypeVMwareHypervisor)
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), sentence) {
			t.Errorf("err = %q, want it to contain %q", err.Error(), sentence)
		}
		if !IsStatus(err, http.StatusForbidden) {
			t.Errorf("IsStatus(err, 403) = false; err = %v", err)
		}
		if errors.Is(err, ErrAuthentication) {
			t.Errorf("an entitlement refusal must not read as an authentication failure; err = %v", err)
		}
	})
}

// THE FILE HOST NEVER SEES THE FORWARD CREDENTIAL. The URL endpoint answers
// with a location on a second server; that server is the witness that no
// Authorization header arrived. Also pins the `as=url` form, the versionId
// pass-through, and that the bytes land intact.
func TestSoftwareCentralDownloadDoesNotForwardCredentials(t *testing.T) {
	payload := bytes.Repeat([]byte("ova-bytes-"), 4096)
	var fileHostAuth, fileHostCookie string
	var fileHostHits int
	fileHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fileHostHits++
		fileHostAuth = r.Header.Get("Authorization")
		fileHostCookie = r.Header.Get("Cookie")
		if r.URL.Path != "/bucket/forward-vm-26.30.0.ova" || r.URL.Query().Get("X-Amz-Signature") != "sig" {
			t.Errorf("file host hit %s", r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer fileHost.Close()

	var gotPath, gotQuery string
	forward := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); !ok {
			t.Errorf("Forward did not receive basic auth")
		}
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"url":"`+fileHost.URL+`/bucket/forward-vm-26.30.0.ova?X-Amz-Signature=sig"}`)
	}))
	defer forward.Close()

	c := newTestClient(t, forward.URL)
	var sink bytes.Buffer
	n, err := c.SoftwareCentral.DownloadDeploymentArtifact(context.Background(), "101", "5001", &sink)
	if err != nil {
		t.Fatalf("DownloadDeploymentArtifact: %v", err)
	}
	if gotPath != "/api/deployment-artifacts/101" {
		t.Errorf("Forward path = %q", gotPath)
	}
	if gotQuery != "as=url&versionId=5001" {
		t.Errorf("Forward query = %q, want as=url&versionId=5001", gotQuery)
	}
	if fileHostHits != 1 {
		t.Fatalf("file host hit %d times, want 1", fileHostHits)
	}
	if fileHostAuth != "" {
		t.Errorf("file host received Authorization %q; the Forward credential leaked to a third party", fileHostAuth)
	}
	if fileHostCookie != "" {
		t.Errorf("file host received Cookie %q", fileHostCookie)
	}
	if n != int64(len(payload)) {
		t.Errorf("wrote %d bytes, want %d", n, len(payload))
	}
	if !bytes.Equal(sink.Bytes(), payload) {
		t.Errorf("downloaded content differs from what the file host served")
	}
}

// An expired or wrong presigned URL is refused by the object store, not by
// Forward, and the refusal is XML-ish plain text. The status and the body
// both have to reach the caller: "403" says where to look, "AccessDenied"
// says why.
func TestSoftwareCentralDownloadSurfacesFileHostRefusal(t *testing.T) {
	fileHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "AccessDenied")
	}))
	defer fileHost.Close()
	forward := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"url":"`+fileHost.URL+`/expired"}`)
	}))
	defer forward.Close()

	c := newTestClient(t, forward.URL)
	var sink bytes.Buffer
	n, err := c.SoftwareCentral.DownloadDeploymentArtifact(context.Background(), "101", "", &sink)
	if err == nil {
		t.Fatal("expected an error from the file host refusal")
	}
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "AccessDenied") {
		t.Errorf("err = %q, want it to name 403 and AccessDenied", err.Error())
	}
	if n != 0 || sink.Len() != 0 {
		t.Errorf("wrote %d bytes (%d in sink) on a refusal, want 0", n, sink.Len())
	}
}

// A blank artifact id must be refused before any request is built, and an
// empty versionId must be OMITTED (latest), not sent as versionId=.
func TestSoftwareCentralURLValidation(t *testing.T) {
	var gotQuery string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"url":"https://files.example/x"}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	ctx := context.Background()

	if _, _, err := c.SoftwareCentral.DeploymentArtifactURL(ctx, " ", "1"); !errors.Is(err, ErrDeploymentArtifactIDRequired) {
		t.Errorf("blank artifact id: err = %v, want ErrDeploymentArtifactIDRequired", err)
	}
	if calls != 0 {
		t.Fatalf("a blank artifact id reached the server (%d calls)", calls)
	}
	got, _, err := c.SoftwareCentral.DeploymentArtifactURL(ctx, "101", "")
	if err != nil || got != "https://files.example/x" {
		t.Fatalf("DeploymentArtifactURL = %q, %v", got, err)
	}
	if gotQuery != "as=url" {
		t.Errorf("query = %q, want as=url with no versionId", gotQuery)
	}
}

// ListForwardApplianceOVAs: only INSTALLATION + VMWARE_HYPERVISOR + OVA
// survive, and the order is newest first. The fixture is built so every
// filter has a victim and the sort has both a timestamp decision and a
// version tie-break to make.
func TestSoftwareCentralListForwardApplianceOVAsFiltersAndSorts(t *testing.T) {
	const catalog = `[
	  {"id":"a","name":"forward-vm","deploymentTypes":["VMWARE_HYPERVISOR","KVM_HYPERVISOR"],"workflow":"INSTALLATION","versions":[
	    {"id":"a1","version":"26.28.0","fileType":"OVA","uploadedAt":100},
	    {"id":"a2","version":"26.30.0","fileType":"OVA","uploadedAt":300},
	    {"id":"a3","version":"26.30.0","fileType":"TAR_GZ","uploadedAt":300},
	    {"id":"a4","version":"26.29.1","fileType":"OVA","uploadedAt":200},
	    {"id":"a5","version":"26.29.0","fileType":"OVA","uploadedAt":200}
	  ]},
	  {"id":"b","name":"forward-vm-kvm","deploymentTypes":["KVM_HYPERVISOR"],"workflow":"INSTALLATION","versions":[
	    {"id":"b1","version":"26.30.0","fileType":"OVA","uploadedAt":999}
	  ]},
	  {"id":"c","name":"forward-app-upgrade","deploymentTypes":["VMWARE_HYPERVISOR"],"workflow":"APP_UPGRADE","versions":[
	    {"id":"c1","baseVersion":"26.29","version":"26.30.0","fileType":"OVA","uploadedAt":999}
	  ]}
	]`
	var gotType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.URL.Query().Get("type")
		_, _ = io.WriteString(w, catalog)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)

	refs, _, err := c.SoftwareCentral.ListForwardApplianceOVAs(context.Background())
	if err != nil {
		t.Fatalf("ListForwardApplianceOVAs: %v", err)
	}
	if gotType != "VMWARE_HYPERVISOR" {
		t.Errorf("server-side filter type = %q", gotType)
	}
	var got []string
	for _, ref := range refs {
		got = append(got, ref.Version.ID.String())
	}
	want := []string{"a2", "a4", "a5", "a1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v (newest UploadedAt first, Version desc on ties; b1 is KVM-only, c1 is an upgrade, a3 is a tarball)", got, want)
	}
	if refs[0].ArtifactID != "a" || refs[0].ArtifactName != "forward-vm" || refs[0].FileName != "forward-vm-26.30.0.ova" {
		t.Errorf("refs[0] = %#v", refs[0])
	}
}

// A cross-origin 302 is what the non-`as=url` form returns, and the client's
// redirect policy refuses it. Pin that a URL endpoint which misbehaves by
// redirecting instead of answering JSON surfaces as an error rather than
// following the hop with the credential attached.
func TestSoftwareCentralURLRefusesCrossOriginRedirect(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("credential followed a cross-origin redirect")
		}
		_, _ = io.WriteString(w, `{"url":"never"}`)
	}))
	defer elsewhere.Close()
	forward := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/file", http.StatusFound)
	}))
	defer forward.Close()

	c := newTestClient(t, forward.URL)
	_, _, err := c.SoftwareCentral.DeploymentArtifactURL(context.Background(), "101", "5001")
	if err == nil || !IsStatus(err, http.StatusFound) {
		t.Fatalf("err = %v, want the 302 surfaced as an API error", err)
	}
}
