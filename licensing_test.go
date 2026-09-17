package forward

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// THE PATHS ARE THE CONTRACT. Forward's controllers map bare paths and this
// client reaches them under /api (verified: CustomBannerController maps
// "/custom-banners" and BannersService calls "/api/custom-banners"). A wrong
// path here does not fail loudly at compile time -- it 404s at runtime against
// a live appserver -- so pin every one.
func TestLicensingPathsAndMethods(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"instanceId":"fp-1","keyStatus":"VALID","licenses":[]}`))
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	ctx := context.Background()

	for _, tc := range []struct {
		name, method, path, query string
		call                      func() error
	}{
		{"Fingerprint", http.MethodGet, "/api/vm/instanceId", "",
			func() error { _, _, err := c.Licensing.Fingerprint(ctx); return err }},
		{"Decode", http.MethodPost, "/api/licenses", "action=decode",
			func() error { _, _, err := c.Licensing.Decode(ctx, "KEY"); return err }},
		{"Apply", http.MethodPost, "/api/licenses", "",
			func() error { _, _, err := c.Licensing.Apply(ctx, "KEY"); return err }},
		{"ListForOrg", http.MethodGet, "/api/orgs/org-1/licenses", "",
			func() error { _, _, err := c.Licensing.ListForOrg(ctx, "org-1"); return err }},
		{"RemoveAllForOrg", http.MethodDelete, "/api/orgs/org-1/licenses", "",
			func() error { _, err := c.Licensing.RemoveAllForOrg(ctx, "org-1"); return err }},
		{"InvalidateForOrg", http.MethodPost, "/api/orgs/org-1/licenses/lic-9", "action=invalidate",
			func() error { _, err := c.Licensing.InvalidateForOrg(ctx, "org-1", "lic-9"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotPath, gotQuery = "", "", ""
			if err := tc.call(); err != nil {
				t.Fatalf("%s returned error: %v", tc.name, err)
			}
			if gotMethod != tc.method || gotPath != tc.path || gotQuery != tc.query {
				t.Errorf("%s hit %s %s?%s, want %s %s?%s",
					tc.name, gotMethod, gotPath, gotQuery, tc.method, tc.path, tc.query)
			}
		})
	}
}

// REMOVE IS NOT DELETE-THE-ORG, and the shape of the request is what makes that
// true. The path must end at the org's LICENSE COLLECTION -- never at the org
// itself -- so an edit that "simplifies" the trailing segment away turns a
// licence removal into something else entirely. Pin it.
func TestLicensingRemoveTargetsTheLicenseCollectionNotTheOrg(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	if _, err := newTestClient(t, server.URL).Licensing.RemoveAllForOrg(context.Background(), "org-1"); err != nil {
		t.Fatalf("RemoveAllForOrg: %v", err)
	}
	if !strings.HasSuffix(path, "/licenses") {
		t.Fatalf("remove hit %q, which does not end at the license collection", path)
	}
	if path == "/api/orgs/org-1" {
		t.Fatal("remove targeted the ORG itself")
	}
}

// A crafted org id must stay inside ONE path element. Without escaping,
// "a/../../orgs/victim" would climb out of the segment and address a different
// org's resources -- the caller would have asked to remove licences from one
// org and the request would arrive somewhere else.
func TestLicensingOrgIDCannotEscapeItsPathSegment(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	if _, err := c.Licensing.RemoveAllForOrg(context.Background(), "a/../../orgs/victim"); err != nil {
		t.Fatalf("RemoveAllForOrg: %v", err)
	}
	if strings.Contains(path, "/orgs/victim/") {
		t.Fatalf("org id escaped its segment: %q", path)
	}
	if !strings.Contains(path, "%2F") {
		t.Fatalf("expected the separator to be escaped, got %q", path)
	}
}

// FAIL BEFORE THE WIRE. An empty org id would build "/api/orgs//licenses";
// what that resolves to server-side is not something to discover by sending it.
// An empty key would come back as a generic decode failure, which reads like a
// bad licence rather than an empty form field.
func TestLicensingRefusesEmptyInputWithoutCallingTheServer(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"licenses":[]}`))
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	ctx := context.Background()

	if _, _, err := c.Licensing.Decode(ctx, "   "); !errors.Is(err, ErrLicenseKeyRequired) {
		t.Errorf("Decode(blank) error = %v, want ErrLicenseKeyRequired", err)
	}
	if _, _, err := c.Licensing.Apply(ctx, ""); !errors.Is(err, ErrLicenseKeyRequired) {
		t.Errorf("Apply(blank) error = %v, want ErrLicenseKeyRequired", err)
	}
	if _, err := c.Licensing.RemoveAllForOrg(ctx, ""); !errors.Is(err, ErrLicenseOrgIDRequired) {
		t.Errorf("RemoveAllForOrg(blank) error = %v, want ErrLicenseOrgIDRequired", err)
	}
	if _, _, err := c.Licensing.ListForOrg(ctx, " "); !errors.Is(err, ErrLicenseOrgIDRequired) {
		t.Errorf("ListForOrg(blank) error = %v, want ErrLicenseOrgIDRequired", err)
	}
	if _, err := c.Licensing.InvalidateForOrg(ctx, "org-1", ""); err == nil {
		t.Error("InvalidateForOrg(blank license) returned no error")
	}
	if called {
		t.Fatal("a blank input reached the server; these must fail before the wire")
	}
	// POSITIVE CONTROL: the same client DOES call out on valid input, so the
	// assertion above is about the guards and not about a dead client.
	if _, _, err := c.Licensing.ListForOrg(ctx, "org-1"); err != nil {
		t.Fatalf("control: ListForOrg(org-1) failed: %v", err)
	}
	if !called {
		t.Fatal("control: a valid call did not reach the server")
	}
}

// An empty instanceId is not a fingerprint. The controller throws rather than
// returning one, so parsing an empty value means we did not understand the
// response -- and binding a licence to "" would fail later, somewhere less
// obvious.
func TestLicensingFingerprintRejectsAnEmptyInstanceID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"instanceId":""}`))
	}))
	defer server.Close()
	if _, _, err := newTestClient(t, server.URL).Licensing.Fingerprint(context.Background()); err == nil {
		t.Fatal("an empty instanceId must be an error, not a fingerprint")
	}
}

// VALID is the only affirmative status, and an EMPTY status is not it. A caller
// that treats "not refused" as "accepted" would activate on a response it never
// understood.
func TestLicenseKeyStatusOKIsAffirmativeOnly(t *testing.T) {
	if !LicenseKeyValid.OK() {
		t.Error("VALID must be OK")
	}
	for _, s := range []LicenseKeyStatus{
		"", LicenseKeyInvalidLicense, LicenseKeyInvalidSignature, LicenseKeyInvalidFingerprint,
		LicenseKeyWrongTier, LicenseKeyBadTierTrialTiming, LicenseKeyBadTrialTier,
	} {
		if s.OK() {
			t.Errorf("status %q must not be OK", s)
		}
	}
}

// The body field is `signedLicenseKey` and the verdict field is `status`, on
// 26.8.x and 26.9 alike (web/.../json/AsciiCodedSignedLicenseKey.java has one
// @JsonCreator parameter of that name and requireNonNull()s it). This client
// sent `key` and read `keyStatus` from 2026-09-08 to 2026-09-17, so decode and
// apply were 400 "'signedLicenseKey' is required" against a real appserver --
// measured live on 26.9.0-09. Both wire shapes are pinned here.
func TestLicensingDecodeWireShapeMatchesForward(t *testing.T) {
	var gotBody map[string]any
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		// 26.9 shape
		_, _ = w.Write([]byte(`{"status":"ALREADY_USED","license":{"tier":"NSP"},"started":true}`))
	})
	out, _, err := c.Licensing.Decode(context.Background(), "LicenseKey__abc_1")
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["signedLicenseKey"] != "LicenseKey__abc_1" {
		t.Fatalf("body = %v, want signedLicenseKey", gotBody)
	}
	if _, present := gotBody["key"]; present {
		t.Fatal("body still carries the old `key` field Forward never read")
	}
	if out.Status != LicenseKeyAlreadyUsed || !out.AlreadyUsed() || out.Started == nil || !*out.Started {
		t.Fatalf("26.9 decode read as %+v", out)
	}
	// 26.8.x shape decodes through the same struct
	c2 := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"VALID","license":{"tier":"NS"},"licenseStatus":"ACTIVE","used":true}`))
	})
	out2, _, err := c2.Licensing.Decode(context.Background(), "LicenseKey__abc_1")
	if err != nil {
		t.Fatal(err)
	}
	if !out2.Status.OK() || !out2.AlreadyUsed() || out2.LicenseStatus != "ACTIVE" {
		t.Fatalf("26.8 decode read as %+v", out2)
	}
}
