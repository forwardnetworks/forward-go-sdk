package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// merakiCall is one request as the fake appserver saw it.
type merakiCall struct {
	Method, Path, Query string
	Body                map[string]any
	RawBody             string
}

// merakiServer records every request and answers with reply. Every Meraki route is under the network, so the recorded path is the
// escaped path the appserver's @PathVariable would see.
func merakiServer(t *testing.T, reply func(w http.ResponseWriter, r *http.Request)) (*Client, func() []merakiCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []merakiCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		call := merakiCall{Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.RawQuery, RawBody: string(raw)}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &call.Body); err != nil {
				t.Errorf("%s %s: body is not a JSON object: %v (%s)", r.Method, r.URL.Path, err, raw)
			}
		}
		mu.Lock()
		calls = append(calls, call)
		mu.Unlock()
		reply(w, r)
	}))
	t.Cleanup(server.Close)
	return newTestClient(t, server.URL), func() []merakiCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]merakiCall(nil), calls...)
	}
}

func onlyCall(t *testing.T, calls []merakiCall) merakiCall {
	t.Helper()
	if len(calls) != 1 {
		t.Fatalf("want exactly one request, got %d: %+v", len(calls), calls)
	}
	return calls[0]
}

func noMerakiRequest(t *testing.T) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected, got %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		w.WriteHeader(http.StatusTeapot)
	}
}

func forwardError(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"httpStatusCode": status, "apiUrl": r.URL.Path, "httpMethod": r.Method, "message": message,
	})
}

// CloudManagedSetupController.getSetups has no type parameter: it returns every cloud-managed setup of the network, each carrying its
// own "type". ListMeraki must therefore keep only the MERAKI ones, so a Mist or VeloCloud setup is never handed to a Meraki reconciler.
func TestMerakiListKeepsOnlyMerakiSetupsOfAMixedListing(t *testing.T) {
	c, calls := merakiServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"type":"MIST","name":"lab-mist","apiKeyId":"AK-2","region":"GLOBAL_01"},
			{"type":"MERAKI","name":"lab-meraki","apiKeyId":"AK-1","credentialId":"H-1","region":"CANADA","hosts":["mx-1"],"collect":false,
			 "testResult":{"savedAt":"2026-11-01T10:00:00Z"}},
			{"type":"VELO_CLOUD","name":"lab-velo","apiKeyId":"AK-3"},
			{"type":"MERAKI","name":"lab-meraki-public","apiKeyId":"AK-4","disablePrivateApiCollection":true,"region":"GLOBAL",
			 "testResult":{"savedAt":"2026-11-01T10:00:00Z"}}
		]`))
	})
	got, _, err := c.CloudManagedSetups.ListMeraki(context.Background(), "net-7")
	if err != nil {
		t.Fatal(err)
	}
	call := onlyCall(t, calls())
	if call.Method != http.MethodGet || call.Path != "/api/networks/net-7/cloud-managed-setups" {
		t.Fatalf("request %s %s, want GET /api/networks/net-7/cloud-managed-setups", call.Method, call.Path)
	}
	if call.Query != "" {
		t.Errorf("query %q: getSetups takes no type parameter, so none should be sent", call.Query)
	}
	if len(got) != 2 || got[0].Name != "lab-meraki" || got[1].Name != "lab-meraki-public" {
		t.Fatalf("kept %+v, want only the two MERAKI setups in order", got)
	}
	first := got[0]
	if first.APIKeyID != "AK-1" || first.CredentialID != "H-1" || first.Region != domain.MerakiRegionCanada ||
		len(first.Hosts) != 1 || first.Hosts[0] != "mx-1" || first.Collect == nil || *first.Collect ||
		first.TestResult == nil || first.TestResult.SavedAt != "2026-11-01T10:00:00Z" {
		t.Errorf("first setup decoded as %+v", first)
	}
	if !got[1].DisablePrivateAPICollection || got[1].CredentialID != "" {
		t.Errorf("public-only setup decoded as %+v", got[1])
	}
}

// GET .../cloud-managed-setups/{name} answers whatever type the setup is. Decoding a Mist setup as Meraki would hand the caller a
// half-empty Meraki setup that a reconciler then "fixes", so GetMeraki must fail instead.
func TestMerakiGetReadsOneSetupAndRefusesAnotherType(t *testing.T) {
	body := `{"type":"MERAKI","name":"lab-meraki","apiKeyId":"AK-1","credentialId":"H-1","region":"GLOBAL","concurrency":4,
		"testResult":{"savedAt":"2026-11-01T10:00:00Z"}}`
	c, calls := merakiServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
	got, _, err := c.CloudManagedSetups.GetMeraki(context.Background(), "net-7", "lab meraki")
	if err != nil {
		t.Fatal(err)
	}
	call := onlyCall(t, calls())
	if call.Method != http.MethodGet || call.Path != "/api/networks/net-7/cloud-managed-setups/lab%20meraki" || call.Query != "" {
		t.Fatalf("request %s %s?%s, want GET .../cloud-managed-setups/lab%%20meraki with no query", call.Method, call.Path, call.Query)
	}
	if got.Name != "lab-meraki" || got.Concurrency == nil || *got.Concurrency != 4 || got.Type != "MERAKI" {
		t.Errorf("decoded %+v", got)
	}

	body = `{"type":"MIST","name":"lab-mist","apiKeyId":"AK-2","region":"GLOBAL_01"}`
	if got, _, err := c.CloudManagedSetups.GetMeraki(context.Background(), "net-7", "lab-mist"); err == nil {
		t.Fatalf("a MIST setup was returned as Meraki: %+v", got)
	}
}

// MerakiSetup's constructor refuses these with a 400; the SDK must refuse them before sending, so a bad desired state never reaches
// Forward (and never half-creates anything).
func TestMerakiCreateRefusesWhatMerakiSetupRefusesBeforeSending(t *testing.T) {
	c, calls := merakiServer(t, noMerakiRequest(t))
	tooMany, zero := domain.MerakiMaxConcurrency+1, 0
	for name, in := range map[string]domain.NewMerakiSetup{
		"no name":                        {APIKeyID: "AK-1", CredentialID: "H-1"},
		"no api key":                     {Name: "lab", CredentialID: "H-1"},
		"no credential, private enabled": {Name: "lab", APIKeyID: "AK-1"},
		"concurrency zero":               {Name: "lab", APIKeyID: "AK-1", CredentialID: "H-1", Concurrency: &zero},
		"concurrency over the maximum":   {Name: "lab", APIKeyID: "AK-1", CredentialID: "H-1", Concurrency: &tooMany},
	} {
		if _, _, err := c.CloudManagedSetups.CreateMeraki(context.Background(), "net-7", in); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if n := len(calls()); n != 0 {
		t.Fatalf("%d requests were sent for refused input", n)
	}
}

// Forward requires testResult (checkNotNull "'testResult' is required"), so CreateMeraki fills one in with the current time when the
// caller gives none. disablePrivateApiCollection is NON_DEFAULT on Forward's side: false is omitted, as Forward writes it.
func TestMerakiCreateSendsForwardsShapeWithATestResult(t *testing.T) {
	c, calls := merakiServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"type":"MERAKI","name":"lab-meraki","apiKeyId":"AK-1","credentialId":"H-1","region":"INDIA",
			"testResult":{"savedAt":"2026-11-01T10:00:00Z"}}`))
	})
	collect, conc := false, domain.MerakiMaxConcurrency
	out, _, err := c.CloudManagedSetups.CreateMeraki(context.Background(), "net-7", domain.NewMerakiSetup{
		Name: "lab-meraki", APIKeyID: "AK-1", CredentialID: "H-1", Region: domain.MerakiRegionIndia, Collect: &collect,
		CollectorID: "col-1", Hosts: []string{"mx-1", "ms-2"}, Concurrency: &conc,
	})
	if err != nil {
		t.Fatal(err)
	}
	call := onlyCall(t, calls())
	if call.Body["type"] != "MERAKI" {
		t.Errorf("create body.type = %#v, want \"MERAKI\" (Forward's setup classes are polymorphic on it)", call.Body["type"])
	}
	if call.Method != http.MethodPost || call.Path != "/api/networks/net-7/cloud-managed-setups" || call.Query != "type=MERAKI" {
		t.Fatalf("request %s %s?%s, want POST .../cloud-managed-setups?type=MERAKI", call.Method, call.Path, call.Query)
	}
	for k, want := range map[string]any{
		"name": "lab-meraki", "apiKeyId": "AK-1", "credentialId": "H-1", "region": "INDIA", "collect": false,
		"collectorId": "col-1", "concurrency": float64(10),
	} {
		if call.Body[k] != want {
			t.Errorf("body.%s = %#v, want %#v", k, call.Body[k], want)
		}
	}
	if hosts, _ := call.Body["hosts"].([]any); len(hosts) != 2 || hosts[0] != "mx-1" || hosts[1] != "ms-2" {
		t.Errorf("body.hosts = %#v", call.Body["hosts"])
	}
	if _, present := call.Body["disablePrivateApiCollection"]; present {
		t.Errorf("body carries disablePrivateApiCollection=%v; false must be omitted", call.Body["disablePrivateApiCollection"])
	}
	tr, ok := call.Body["testResult"].(map[string]any)
	if !ok {
		t.Fatalf("body.testResult = %#v; Forward refuses a setup without one", call.Body["testResult"])
	}
	savedAt, _ := tr["savedAt"].(string)
	if _, err := time.Parse(time.RFC3339, savedAt); err != nil {
		t.Errorf("testResult.savedAt = %q is not an RFC 3339 instant: %v", savedAt, err)
	}
	if _, present := tr["error"]; present {
		t.Errorf("a defaulted test result must carry no error (a null error means it passed): %#v", tr)
	}
	if out.Name != "lab-meraki" || out.Region != domain.MerakiRegionIndia {
		t.Errorf("decoded %+v", out)
	}
}

// A public-API-only setup needs no dashboard login: the constructor accepts a missing credentialId exactly when
// disablePrivateApiCollection is set, so true must reach the wire, and a caller's own test result is sent as given.
func TestMerakiCreatePublicOnlySendsTheFlagAndTheCallersTestResult(t *testing.T) {
	c, calls := merakiServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"type":"MERAKI","name":"lab-public","apiKeyId":"AK-1","disablePrivateApiCollection":true}`))
	})
	_, _, err := c.CloudManagedSetups.CreateMeraki(context.Background(), "net-7", domain.NewMerakiSetup{
		Name: "lab-public", APIKeyID: "AK-1", DisablePrivateAPICollection: true,
		TestResult: &domain.MerakiTestResult{SavedAt: "2026-10-31T08:15:00Z", OTPBypassToken: "otp-x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	call := onlyCall(t, calls())
	if call.Body["disablePrivateApiCollection"] != true {
		t.Errorf("body.disablePrivateApiCollection = %#v, want true", call.Body["disablePrivateApiCollection"])
	}
	if _, present := call.Body["credentialId"]; present {
		t.Errorf("body carries credentialId %#v; none was given", call.Body["credentialId"])
	}
	tr, _ := call.Body["testResult"].(map[string]any)
	if tr["savedAt"] != "2026-10-31T08:15:00Z" || tr["otpBypassToken"] != "otp-x" {
		t.Errorf("body.testResult = %#v, want the caller's own", call.Body["testResult"])
	}
}

// MerakiSetupPatch is JsonProp: an absent property leaves the stored value and an explicit null resets it. PatchMeraki must send
// exactly the fields that were set plus a null for each Clear flag, on PATCH ...?type=MERAKI.
func TestMerakiPatchSendsOnlySetFieldsAndNullsForClears(t *testing.T) {
	c, calls := merakiServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"type":"MERAKI","name":"lab-meraki","apiKeyId":"AK-1","disablePrivateApiCollection":true}`))
	})
	disable, collect := true, true
	empty := []string{}
	out, _, err := c.CloudManagedSetups.PatchMeraki(context.Background(), "net-7", "lab-meraki", domain.MerakiSetupPatch{
		DisablePrivateAPICollection: &disable, Collect: &collect, Hosts: &empty, Region: domain.MerakiRegionGovernment,
		ClearCredential: true, ClearCollector: true, ClearConcurrency: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	call := onlyCall(t, calls())
	if call.Method != http.MethodPatch || call.Path != "/api/networks/net-7/cloud-managed-setups/lab-meraki" || call.Query != "type=MERAKI" {
		t.Fatalf("request %s %s?%s, want PATCH .../cloud-managed-setups/lab-meraki?type=MERAKI", call.Method, call.Path, call.Query)
	}
	// "type":"MERAKI" is Forward's polymorphic discriminator, which its own tests and GUI always send; it is not a changed field.
	if call.Body["type"] != "MERAKI" {
		t.Errorf("body.type = %#v, want \"MERAKI\"", call.Body["type"])
	}
	delete(call.Body, "type")
	want := map[string]bool{
		"disablePrivateApiCollection": true, "collect": true, "hosts": true, "region": true,
		"credentialId": true, "collectorId": true, "concurrency": true,
	}
	for k := range call.Body {
		if !want[k] {
			t.Errorf("body carries %q = %#v, which was not set; an absent field must stay absent", k, call.Body[k])
		}
	}
	for k := range want {
		if _, present := call.Body[k]; !present {
			t.Errorf("body lacks %q", k)
		}
	}
	for _, k := range []string{"credentialId", "collectorId", "concurrency"} {
		if v, present := call.Body[k]; present && v != nil {
			t.Errorf("body.%s = %#v, want an explicit null", k, v)
		}
	}
	if call.Body["disablePrivateApiCollection"] != true || call.Body["collect"] != true || call.Body["region"] != "GOVERNMENT" {
		t.Errorf("body = %s", call.RawBody)
	}
	if hosts, ok := call.Body["hosts"].([]any); !ok || len(hosts) != 0 {
		t.Errorf("body.hosts = %#v, want [] (collect every discovered host)", call.Body["hosts"])
	}
	if !out.DisablePrivateAPICollection {
		t.Errorf("decoded %+v", out)
	}
}

// A patch that both sets a field and asks for it to be reset says two things at once; JSON can carry only one, so whichever won
// would silently drop the other. PatchMeraki must refuse it before sending.
func TestMerakiPatchRefusesSettingAndClearingOneField(t *testing.T) {
	c, calls := merakiServer(t, noMerakiRequest(t))
	four := 4
	for name, p := range map[string]domain.MerakiSetupPatch{
		"credential":  {CredentialID: "H-2", ClearCredential: true},
		"collector":   {CollectorID: "col-2", ClearCollector: true},
		"concurrency": {Concurrency: &four, ClearConcurrency: true},
	} {
		if _, _, err := c.CloudManagedSetups.PatchMeraki(context.Background(), "net-7", "lab-meraki", p); err == nil {
			t.Errorf("%s: set and cleared together was accepted", name)
		}
	}
	if n := len(calls()); n != 0 {
		t.Fatalf("%d requests were sent for contradictory patches", n)
	}
}

// The test route's body is CloudManagedConnectivityRequest, polymorphic on an existing "type" property: without "type":"MERAKI"
// Jackson cannot pick MerakiSetupConnectivityTestRequest. Its "id" is the setup name. Forward answers 204 with no body.
func TestMerakiTestSendsTheTypedBodyAndAccepts204(t *testing.T) {
	c, calls := merakiServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	_, err := c.CloudManagedSetups.TestMeraki(context.Background(), "net-7", domain.MerakiConnectivityTest{
		Name: "lab-meraki", APIKeyID: "AK-1", CredentialID: "H-1", ProxyServerID: "px-1", Region: domain.MerakiRegionChina,
		CollectorID: "col-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	call := onlyCall(t, calls())
	if call.Method != http.MethodPost || call.Path != "/api/networks/net-7/cloud-managed-setup-tests" || call.Query != "type=MERAKI" {
		t.Fatalf("request %s %s?%s, want POST .../cloud-managed-setup-tests?type=MERAKI", call.Method, call.Path, call.Query)
	}
	for k, want := range map[string]any{
		"type": "MERAKI", "id": "lab-meraki", "apiKeyId": "AK-1", "credentialId": "H-1", "proxyServerId": "px-1",
		"region": "CHINA", "collectorId": "col-1",
	} {
		if call.Body[k] != want {
			t.Errorf("body.%s = %#v, want %#v", k, call.Body[k], want)
		}
	}
	if _, present := call.Body["name"]; present {
		t.Errorf("body carries name; the request's field is id")
	}
	if v, present := call.Body["disablePrivateApiCollection"]; present && v != false {
		t.Errorf("body.disablePrivateApiCollection = %#v, want false or absent", v)
	}
}

// discover answers DiscoveredMerakiHosts, {"hosts":[...]} of DiscoveredMerakiHost {name, model, orgId, orgName}, on
// POST {name}?action=discover&type=MERAKI with no body.
func TestMerakiDiscoverDecodesForwardsHosts(t *testing.T) {
	c, calls := merakiServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"hosts":[{"name":"mx-1","model":"MX","orgId":"o-1","orgName":"Example Org"},{"name":"ms-2","model":"MS"}]}`))
	})
	out, _, err := c.CloudManagedSetups.DiscoverMeraki(context.Background(), "net-7", "lab-meraki")
	if err != nil {
		t.Fatal(err)
	}
	call := onlyCall(t, calls())
	if call.Method != http.MethodPost || call.Path != "/api/networks/net-7/cloud-managed-setups/lab-meraki" {
		t.Fatalf("request %s %s, want POST .../cloud-managed-setups/lab-meraki", call.Method, call.Path)
	}
	if q := call.Query; q != "action=discover&type=MERAKI" && q != "type=MERAKI&action=discover" {
		t.Errorf("query %q, want action=discover and type=MERAKI", q)
	}
	if call.RawBody != "" {
		t.Errorf("discover sent a body %q", call.RawBody)
	}
	want := []domain.MerakiDiscoveredHost{{Name: "mx-1", Model: "MX", OrgID: "o-1", OrgName: "Example Org"}, {Name: "ms-2", Model: "MS"}}
	if len(out.Hosts) != len(want) || out.Hosts[0] != want[0] || out.Hosts[1] != want[1] {
		t.Fatalf("decoded %+v, want %+v", out.Hosts, want)
	}
}

// deleteSetup answers 204, or 404 "Network has no setup named ..." when it is already gone; both mean the desired state (absent)
// holds. A 403 is a refusal, not absence, and must reach the caller as Forward's typed permission error.
func TestMerakiDeleteTreatsMissingAsSuccessButNotForbidden(t *testing.T) {
	status := http.StatusNoContent
	c, calls := merakiServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch status {
		case http.StatusNotFound:
			forwardError(w, r, status, "Network has no setup named lab-meraki")
		case http.StatusForbidden:
			forwardError(w, r, status, "Missing permission: NetworkOperation.DELETE_COLLECTION_SOURCES")
		default:
			w.WriteHeader(status)
		}
	})
	if _, err := c.CloudManagedSetups.DeleteMeraki(context.Background(), "net-7", "lab-meraki"); err != nil {
		t.Fatalf("204: %v", err)
	}
	call := onlyCall(t, calls())
	if call.Method != http.MethodDelete || call.Path != "/api/networks/net-7/cloud-managed-setups/lab-meraki" {
		t.Fatalf("request %s %s, want DELETE .../cloud-managed-setups/lab-meraki", call.Method, call.Path)
	}
	// deleteSetup maps no params; an extra type=MERAKI would be ignored, anything else is a different call.
	if call.Query != "" && call.Query != "type=MERAKI" {
		t.Errorf("query %q", call.Query)
	}

	status = http.StatusNotFound
	if _, err := c.CloudManagedSetups.DeleteMeraki(context.Background(), "net-7", "lab-meraki"); err != nil {
		t.Fatalf("404 for a missing setup must be success: %v", err)
	}

	status = http.StatusForbidden
	_, err := c.CloudManagedSetups.DeleteMeraki(context.Background(), "net-7", "lab-meraki")
	if err == nil {
		t.Fatal("403 was treated as success")
	}
	if !ports.IsStatus(err, http.StatusForbidden) || !errors.Is(err, domain.ErrPermissionDenied) {
		t.Errorf("403 error %v is not the typed permission error", err)
	}
}

// Forward answers 404 both for a setup that is not there ("Network has no setup named ...", which a delete treats as done) and
// for a network that does not exist ("Network not found"). Reading the second as "deleted" would report success for a delete
// aimed at a mistyped network ID, so only the first counts as done.
func TestMerakiDeleteDoesNotTreatAMissingNetworkAsDeleted(t *testing.T) {
	c, _ := merakiServer(t, func(w http.ResponseWriter, r *http.Request) {
		forwardError(w, r, http.StatusNotFound, "Network not found")
	})
	_, err := c.CloudManagedSetups.DeleteMeraki(context.Background(), "no-such-net", "lab-meraki")
	if err == nil || !errors.Is(err, domain.ErrNetworkNotFound) {
		t.Fatalf("DeleteMeraki on a missing network = %v, want an error that is ErrNetworkNotFound", err)
	}
}

// GetMeraki refuses another kind of setup, and names what it found; a setup that states no type is said to be untyped, not
// shown as an empty type.
func TestMerakiGetNamesTheKindItRefused(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"name":"x","type":"MIST"}`, "has type MIST"},
		{`{"name":"x"}`, "has type none"},
	} {
		c, _ := merakiServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(tc.body))
		})
		_, _, err := c.CloudManagedSetups.GetMeraki(context.Background(), "net-7", "x")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("GetMeraki(%s) = %v, want an error containing %q", tc.body, err, tc.want)
		}
	}
}
