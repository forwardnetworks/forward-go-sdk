package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/forwardnetworks/forward-go-sdk/domain"
)

func intPtr(n int) *int { return &n }

// Validate mirrors MerakiSetup's constructor (and CloudManagedSetup's): name and apiKeyId are required, credentialId is required
// unless disablePrivateApiCollection is set, and concurrency, when given, must lie in [1, MAX_API_CONCURRENCY]. A missing test result
// is not refused here, because CreateMeraki supplies one.
func TestNewMerakiSetupValidateMirrorsTheConstructor(t *testing.T) {
	base := domain.NewMerakiSetup{Name: "lab", APIKeyID: "AK-1", CredentialID: "H-1"}
	refused := map[string]func(*domain.NewMerakiSetup){
		"no name":                      func(s *domain.NewMerakiSetup) { s.Name = "" },
		"no api key":                   func(s *domain.NewMerakiSetup) { s.APIKeyID = "" },
		"no credential, private on":    func(s *domain.NewMerakiSetup) { s.CredentialID = "" },
		"concurrency zero":             func(s *domain.NewMerakiSetup) { s.Concurrency = intPtr(0) },
		"concurrency negative":         func(s *domain.NewMerakiSetup) { s.Concurrency = intPtr(-1) },
		"concurrency over the maximum": func(s *domain.NewMerakiSetup) { s.Concurrency = intPtr(domain.MerakiMaxConcurrency + 1) },
	}
	for name, mutate := range refused {
		s := base
		mutate(&s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: accepted %+v", name, s)
		}
	}
	accepted := map[string]func(*domain.NewMerakiSetup){
		"as is":                                  func(*domain.NewMerakiSetup) {},
		"public only, no credential":             func(s *domain.NewMerakiSetup) { s.CredentialID = ""; s.DisablePrivateAPICollection = true },
		"concurrency one":                        func(s *domain.NewMerakiSetup) { s.Concurrency = intPtr(1) },
		"concurrency at the maximum":             func(s *domain.NewMerakiSetup) { s.Concurrency = intPtr(domain.MerakiMaxConcurrency) },
		"no test result (CreateMeraki fills it)": func(s *domain.NewMerakiSetup) { s.TestResult = nil },
	}
	for name, mutate := range accepted {
		s := base
		mutate(&s)
		if err := s.Validate(); err != nil {
			t.Errorf("%s: refused %+v: %v", name, s, err)
		}
	}
	if domain.MerakiMaxConcurrency != 10 {
		t.Errorf("MerakiMaxConcurrency = %d; MerakiSetup.MAX_API_CONCURRENCY is 10", domain.MerakiMaxConcurrency)
	}
}

func marshalObject(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %+v: %v", v, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s is not a JSON object: %v", raw, err)
	}
	// "type" is Forward's polymorphic discriminator, not a field of the update: a patch of a Meraki setup always names it, and
	// it must say MERAKI. It is checked here and removed, so the tests that follow see only the fields the caller set.
	if _, isPatch := v.(domain.MerakiSetupPatch); isPatch {
		if out["type"] != "MERAKI" {
			t.Errorf("a patch must carry \"type\":\"MERAKI\" (Forward's setup classes are polymorphic on it): %s", raw)
		}
		delete(out, "type")
	}
	return out
}

// JsonProp: an absent property leaves the stored value untouched, so the zero patch must be the empty object; each Clear flag must
// write an explicit null (the reset), and a set field its value, with nothing else on the wire.
func TestMerakiSetupPatchMarshalsAbsentSetAndNull(t *testing.T) {
	if got := marshalObject(t, domain.MerakiSetupPatch{}); len(got) != 0 {
		t.Errorf("zero patch = %v, want {}", got)
	}

	off := false
	got := marshalObject(t, domain.MerakiSetupPatch{DisablePrivateAPICollection: &off})
	if v, present := got["disablePrivateApiCollection"]; !present || v != false || len(got) != 1 {
		t.Errorf("an explicit false must be sent (it turns private collection back on): %v", got)
	}

	conc := 3
	hosts := []string{"mx-1"}
	got = marshalObject(t, domain.MerakiSetupPatch{
		Name: "lab-renamed", APIKeyID: "AK-2", CredentialID: "H-2", CollectorID: "col-2", Concurrency: &conc, Hosts: &hosts,
		Region: domain.MerakiRegionGlobal,
	})
	want := map[string]any{
		"name": "lab-renamed", "apiKeyId": "AK-2", "credentialId": "H-2", "collectorId": "col-2", "concurrency": float64(3),
		"region": "GLOBAL",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %#v, want %#v", k, got[k], w)
		}
	}
	if h, _ := got["hosts"].([]any); len(h) != 1 || h[0] != "mx-1" {
		t.Errorf("hosts = %#v", got["hosts"])
	}
	if len(got) != len(want)+1 {
		t.Errorf("patch carries extra properties: %v", got)
	}

	for flag, p := range map[string]domain.MerakiSetupPatch{
		"credentialId": {ClearCredential: true},
		"collectorId":  {ClearCollector: true},
		"concurrency":  {ClearConcurrency: true},
	} {
		got := marshalObject(t, p)
		if v, present := got[flag]; !present || v != nil || len(got) != 1 {
			t.Errorf("clear %s = %v, want exactly {%q:null}", flag, got, flag)
		}
	}
}

// The test route's body is polymorphic on "type": MarshalJSON must write "type":"MERAKI" and the setup name as "id"
// (MerakiSetupConnectivityTestRequest checks 'id' is required), with empty optionals left out.
func TestMerakiConnectivityTestMarshalsTypeAndID(t *testing.T) {
	got := marshalObject(t, domain.MerakiConnectivityTest{Name: "lab", APIKeyID: "AK-1", DisablePrivateAPICollection: true})
	if got["type"] != "MERAKI" || got["id"] != "lab" || got["apiKeyId"] != "AK-1" || got["disablePrivateApiCollection"] != true {
		t.Errorf("body = %v", got)
	}
	for _, absent := range []string{"name", "credentialId", "proxyServerId", "otpBypassToken", "region", "collectorId"} {
		if _, present := got[absent]; present {
			t.Errorf("body carries %q = %#v", absent, got[absent])
		}
	}
}

// DiscoveredMerakiHosts is {"hosts":[DiscoveredMerakiHost]} with name, model, orgId, orgName; an answer with no org fields (both
// @Nullable) still decodes.
func TestMerakiDiscoveryDecodesForwardsShape(t *testing.T) {
	var d domain.MerakiDiscovery
	if err := json.Unmarshal([]byte(`{"hosts":[{"name":"mx-1","model":"MX","orgId":"o-1","orgName":"Example Org"},{"name":"mr-2","model":"MR"}]}`), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Hosts) != 2 || d.Hosts[0] != (domain.MerakiDiscoveredHost{Name: "mx-1", Model: "MX", OrgID: "o-1", OrgName: "Example Org"}) ||
		d.Hosts[1] != (domain.MerakiDiscoveredHost{Name: "mr-2", Model: "MR"}) {
		t.Errorf("decoded %+v", d.Hosts)
	}
}

// Forward's setup classes are polymorphic on "type" (CloudManagedSetup is @JsonTypeInfo(property = "type", include =
// EXISTING_PROPERTY)), its own controller tests and GUI always send it, and Jackson needs a type id for a subtype by default, so
// a create body without "type":"MERAKI" risks a 400. The rest of the body is unchanged.
func TestNewMerakiSetupCarriesTheTypeDiscriminator(t *testing.T) {
	raw, err := json.Marshal(domain.NewMerakiSetup{Name: "lab", APIKeyID: "AK-1", CredentialID: "H-1"})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out["type"] != "MERAKI" || out["name"] != "lab" || out["apiKeyId"] != "AK-1" || out["credentialId"] != "H-1" {
		t.Errorf("create body = %s", raw)
	}
	if _, present := out["disablePrivateApiCollection"]; present {
		t.Errorf("disablePrivateApiCollection must be omitted when false: %s", raw)
	}
}
