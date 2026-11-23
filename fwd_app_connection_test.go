package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The contract (FwdAppConnectionController): the connection test is a POST with action=test and no body, answering {"status": SUCCESS |
// AUTH_FAILED | CONNECTION_FAILED} as a 200 even when the connection fails; the CVE update is a POST with action=update answering
// {"error": CONNECTION_FAILED | INSTALL_FAILED} or {} on success, also a 200; the proxy read is a GET whose body is the proxy settings with
// the credential's username only.
func TestFwdAppConnectionAndCVEIndexUpdate(t *testing.T) {
	var method, uri, body string
	reply := `{"status":"SUCCESS"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		method, uri, body = r.Method, r.URL.RequestURI(), string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	test, _, err := client.FwdAppConnection.Test(ctx)
	if err != nil || !test.OK() || test.Status != "SUCCESS" {
		t.Fatalf("test=%+v err=%v", test, err)
	}
	if method != http.MethodPost || uri != "/api/fwd-app-connection?action=test" || body != "" {
		t.Fatalf("sent %s %s body=%q", method, uri, body)
	}
	reply = `{"status":"AUTH_FAILED"}`
	if failed, _, err := client.FwdAppConnection.Test(ctx); err != nil || failed.OK() || failed.Status != domain.FwdAppConnectionAuthFailed {
		t.Fatalf("an authentication failure is a 200 with a status, not an error: %+v %v", failed, err)
	}

	reply = `{}`
	update, _, err := client.CVEIndex.UpdateFromSoftwareCentral(ctx)
	if err != nil || !update.OK() {
		t.Fatalf("update=%+v err=%v", update, err)
	}
	if method != http.MethodPost || uri != "/api/cve-index?action=update" || body != "" {
		t.Fatalf("sent %s %s body=%q", method, uri, body)
	}
	reply = `{"error":"INSTALL_FAILED"}`
	if failed, _, err := client.CVEIndex.UpdateFromSoftwareCentral(ctx); err != nil || failed.OK() || failed.Error != domain.CVEIndexUpdateInstallFailed {
		t.Fatalf("a failed update is a 200 with an error name: %+v %v", failed, err)
	}

	reply = `{"enabled":true,"url":"https://proxy.example:3128","verifyCertificate":false,"enableAuth":true,"username":"svc","createdBy":"admin"}`
	proxy, _, err := client.FwdAppConnection.Proxy(ctx)
	if err != nil || !proxy.Enabled || proxy.URL != "https://proxy.example:3128" || proxy.VerifyCertificate == nil || *proxy.VerifyCertificate || proxy.EnableAuth == nil || !*proxy.EnableAuth || proxy.Username != "svc" {
		t.Fatalf("proxy=%+v err=%v", proxy, err)
	}
	if method != http.MethodGet || uri != "/api/fwd-app-proxy" || len(proxy.Raw) == 0 {
		t.Fatalf("sent %s %s raw=%s", method, uri, proxy.Raw)
	}
	reply = `{"enabled":false}`
	if def, _, err := client.FwdAppConnection.Proxy(ctx); err != nil || def.Enabled || def.URL != "" {
		t.Fatalf("default proxy = %+v %v", def, err)
	}
}

// A SaaS deployment does not map these on-prem routes: the answer is Forward's own 404, which classifies as endpoint-not-served.
func TestFwdAppConnectionNotServedOnSaaS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No endpoint ` + r.Method + ` ` + r.URL.RequestURI() + `."}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	if _, _, err := client.FwdAppConnection.Test(context.Background()); !ports.IsErrorKind(err, domain.ErrorKindEndpointNotServed) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := client.CVEIndex.UpdateFromSoftwareCentral(context.Background()); !ports.IsErrorKind(err, domain.ErrorKindEndpointNotServed) {
		t.Fatalf("err = %v", err)
	}
}
