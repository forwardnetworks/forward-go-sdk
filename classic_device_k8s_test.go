package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The k8s types are UNDER_DEVELOPMENT, which Forward does not expose: Exposed must follow Forward's own isExposed() rule
// (PRODUCTION and TECH_PREVIEW only) so a caller can warn before onboarding a type that only the DEV parser turns into devices.
func TestConnTypeInfoExposedFollowsForwardsRule(t *testing.T) {
	for status, want := range map[string]bool{
		domain.ConnTypeStatusProduction: true, domain.ConnTypeStatusTechPreview: true,
		domain.ConnTypeStatusInternal: false, domain.ConnTypeStatusUnderDevelopment: false, domain.ConnTypeStatusNotSupported: false, "": false,
	} {
		if got := (domain.ConnTypeInfo{Status: status}).Exposed(); got != want {
			t.Errorf("Exposed() for %q = %v, want %v", status, got, want)
		}
	}
	if domain.ConnTypeK8sCniControlPlaneAPI != "k8s_cni_control_plane_api" || domain.ConnTypeK8sCniNodeAPI != "k8s_cni_node_api" || domain.ConnTypeK8sCniPodAPI != "k8s_cni_pod_api" {
		t.Error("a k8s type constant does not carry Forward's wire id")
	}
}

// ConnTypesExposed must send no for= parameter: that is how Forward narrows the list to exposed types, while ConnTypes (for=ui) is the wide one.
func TestConnTypesExposedSendsNoForParameter(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RequestURI()
		_, _ = io.WriteString(w, `{"b_ssh":{"status":"PRODUCTION"},"a_api":{"status":"TECH_PREVIEW"}}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	got, _, err := c.ClassicDevices.ConnTypesExposed(context.Background())
	if err != nil || seen != "/api/deviceConnTypes" {
		t.Fatalf("err=%v request=%q, want a bare /api/deviceConnTypes", err, seen)
	}
	if len(got) != 2 || got[0].ID != "a_api" || got[1].ID != "b_ssh" {
		t.Fatalf("not sorted by id or ids not filled: %+v", got)
	}
}

// CreateWithAPIKey must send the credential as API_KEY with no username, then a device whose httpCredentialId is the id Forward
// just minted; and when the device create fails the caller must still get the credential, because it was left behind.
func TestCreateWithAPIKeyChainsCredentialIntoDevice(t *testing.T) {
	var paths, bodies []string
	failDevice := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		paths = append(paths, r.Method+" "+r.URL.Path)
		bodies = append(bodies, string(b))
		switch {
		case strings.HasSuffix(r.URL.Path, "/http-credentials"):
			_, _ = io.WriteString(w, `{"id":"AK-0","type":"API_KEY","name":"k"}`)
		case failDevice:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"bad"}`)
		default:
			_, _ = io.WriteString(w, `{"name":"k8s1","type":"k8s_cni_control_plane_api"}`)
		}
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	ctx := context.Background()
	dev := domain.ClassicDeviceRequest{Name: "k8s1", Host: "h", Type: domain.ConnTypeK8sCniControlPlaneAPI}

	out, _, err := c.ClassicDevices.CreateWithAPIKey(ctx, "6272", domain.HTTPCredentialRequest{Name: "k", Password: "tok"}, dev)
	if err != nil || out.Credential.ID != "AK-0" || out.Device.Name != "k8s1" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if !strings.Contains(bodies[0], `"type":"API_KEY"`) || strings.Contains(bodies[0], "username") {
		t.Errorf("credential body = %s, want API_KEY and no username", bodies[0])
	}
	if !strings.Contains(bodies[1], `"httpCredentialId":"AK-0"`) {
		t.Errorf("device body = %s, want the new credential id", bodies[1])
	}

	failDevice = true
	out, _, err = c.ClassicDevices.CreateWithAPIKey(ctx, "6272", domain.HTTPCredentialRequest{Name: "k", Password: "tok"}, dev)
	if err == nil || out == nil || out.Credential == nil || out.Credential.ID != "AK-0" {
		t.Errorf("a failed device create must still return the orphaned credential: out=%+v err=%v", out, err)
	}

	n := len(paths)
	if _, _, err := c.ClassicDevices.CreateWithAPIKey(ctx, "6272", domain.HTTPCredentialRequest{Type: "LOGIN", Name: "k", Password: "p"}, dev); err == nil {
		t.Error("a LOGIN credential must be refused")
	}
	if len(paths) != n {
		t.Error("a refused call must send nothing")
	}
}
