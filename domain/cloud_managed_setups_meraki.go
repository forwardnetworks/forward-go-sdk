package domain

import (
	"encoding/json"
	"errors"
	"strings"
)

// Meraki cloud-managed setups: a Meraki dashboard account whose devices Forward collects through Meraki's APIs, not device by
// device. Preview: verified against web/.../controller/CloudManagedSetupController.java (the type=MERAKI create, patch, test and
// discover mappings), app/.../sources/cloudmanaged/MerakiSetup.java and MerakiSetupPatch.java, and client-structs/.../meraki/
// MerakiRegion.java, at Forward master 54dac810c156 and at both pinned builds (5f7e4ec1841, b1feb6fe1c34).
//
// Two collection modes. With the private APIs (the default), Forward collects classic MX, MS and MR devices and Catalyst; it needs
// the API key and a dashboard login, and the dashboard account must have two-factor sign-in off. With DisablePrivateAPICollection
// (Forward 26.11), only the public API is used: the API key alone, two-factor sign-in may stay on, and only Catalyst routers on
// IOS-XE 26.1 or later and switches on 17.18 or later are collected; classic devices then fail with "Device type not supported".

// MerakiRegion is where the Meraki dashboard is hosted; the collector derives the API and dashboard URLs from it. Forward
// defaults an unset region to GLOBAL.
type MerakiRegion string

// The regions Forward knows (MerakiRegion.java).
const (
	MerakiRegionGlobal     MerakiRegion = "GLOBAL"     // api.meraki.com
	MerakiRegionChina      MerakiRegion = "CHINA"      // api.meraki.cn
	MerakiRegionCanada     MerakiRegion = "CANADA"     // api.meraki.ca
	MerakiRegionIndia      MerakiRegion = "INDIA"      // api.meraki.in
	MerakiRegionGovernment MerakiRegion = "GOVERNMENT" // api.gov-meraki.com
)

// MerakiMaxConcurrency is the most API calls Forward lets one Meraki setup make at once (MerakiSetup.MAX_API_CONCURRENCY).
const MerakiMaxConcurrency = 10

// NewMerakiSetup is the body of POST .../cloud-managed-setups?type=MERAKI.
type NewMerakiSetup struct {
	Name string `json:"name"`
	// APIKeyID is the id of the HTTP credential holding the Meraki API key (Credentials.CreateHTTP). Required.
	APIKeyID string `json:"apiKeyId"`
	// CredentialID is the id of a LOGIN HTTP credential for the dashboard, which the private APIs use. Required unless
	// DisablePrivateAPICollection is set (MerakiSetup's own check).
	CredentialID string `json:"credentialId,omitempty"`
	// DisablePrivateAPICollection skips the private dashboard APIs and collects through the public API only (Forward 26.11).
	// It is omitted when false, as Forward writes it.
	DisablePrivateAPICollection bool `json:"disablePrivateApiCollection,omitempty"`
	// Region selects the dashboard's hosting region; empty means GLOBAL.
	Region MerakiRegion `json:"region,omitempty"`
	// Collect: nil or true takes part in collections; false parks the setup.
	Collect *bool `json:"collect,omitempty"`
	// CollectorID pins the collector; empty means the network's default one.
	CollectorID string `json:"collectorId,omitempty"`
	// Hosts limits collection to these devices; empty collects every device discovered at collection time.
	Hosts []string `json:"hosts,omitempty"`
	// Concurrency caps concurrent API calls, from 1 to MerakiMaxConcurrency; nil leaves Forward's default.
	Concurrency *int `json:"concurrency,omitempty"`
	// TestResult is required by Forward (a setup records its last connectivity test). Leave it nil and CreateMeraki sends the
	// current time with no error.
	TestResult *MerakiTestResult `json:"testResult,omitempty"`
}

// MarshalJSON writes the body with "type":"MERAKI". Forward's setup classes are polymorphic on that property
// (CloudManagedSetup is @JsonTypeInfo(property = "type", include = EXISTING_PROPERTY)), its own tests and its GUI always send
// it, and Jackson requires a type id for a subtype by default, so a body without it is at risk of a 400.
func (s NewMerakiSetup) MarshalJSON() ([]byte, error) {
	type plain NewMerakiSetup
	return json.Marshal(struct {
		Type string `json:"type"`
		plain
	}{"MERAKI", plain(s)})
}

// Validate refuses what Forward's MerakiSetup constructor refuses, before anything is sent.
func (s NewMerakiSetup) Validate() error {
	switch {
	case strings.TrimSpace(s.Name) == "":
		return errors.New("forward: meraki setup name is required")
	case strings.TrimSpace(s.APIKeyID) == "":
		return errors.New("forward: meraki setup apiKeyId is required (the HTTP credential holding the API key)")
	case strings.TrimSpace(s.CredentialID) == "" && !s.DisablePrivateAPICollection:
		return errors.New("forward: meraki setup credentialId is required unless DisablePrivateAPICollection is set")
	case s.Concurrency != nil && (*s.Concurrency < 1 || *s.Concurrency > MerakiMaxConcurrency):
		return errors.New("forward: meraki setup concurrency must be from 1 to 10 when set")
	}
	return nil
}

// MerakiSetup is a Meraki setup as Forward returns it.
type MerakiSetup struct {
	Name                        string            `json:"name"`
	Type                        string            `json:"type,omitempty"`
	APIKeyID                    string            `json:"apiKeyId,omitempty"`
	CredentialID                string            `json:"credentialId,omitempty"`
	DisablePrivateAPICollection bool              `json:"disablePrivateApiCollection,omitempty"`
	Region                      MerakiRegion      `json:"region,omitempty"`
	Collect                     *bool             `json:"collect,omitempty"`
	CollectorID                 string            `json:"collectorId,omitempty"`
	Hosts                       []string          `json:"hosts,omitempty"`
	Concurrency                 *int              `json:"concurrency,omitempty"`
	TestResult                  *MerakiTestResult `json:"testResult,omitempty"`
}

// MerakiTestResult is the last connectivity test a setup records. A nil Error means it passed.
type MerakiTestResult struct {
	SavedAt         string                 `json:"savedAt,omitempty"`
	Error           json.RawMessage        `json:"error,omitempty"`
	DiscoveredHosts []MerakiDiscoveredHost `json:"discoveredHosts,omitempty"`
	OTPBypassToken  string                 `json:"otpBypassToken,omitempty"`
}

// MerakiDiscoveredHost is one device as Meraki discovery reports it (DiscoveredMerakiHost).
type MerakiDiscoveredHost struct {
	Name    string `json:"name"`
	Model   string `json:"model,omitempty"`
	OrgID   string `json:"orgId,omitempty"`
	OrgName string `json:"orgName,omitempty"`
}

// MerakiDiscovery is the body of a discover answer (DiscoveredMerakiHosts).
type MerakiDiscovery struct {
	Hosts []MerakiDiscoveredHost `json:"hosts"`
}

// MerakiConnectivityTest is the body of POST .../cloud-managed-setup-tests?type=MERAKI (MerakiSetupConnectivityTestRequest):
// it tests the credentials against Meraki before or after the setup is saved. Name is the setup's name.
type MerakiConnectivityTest struct {
	Name                        string       `json:"id"`
	APIKeyID                    string       `json:"apiKeyId"`
	CredentialID                string       `json:"credentialId,omitempty"`
	ProxyServerID               string       `json:"proxyServerId,omitempty"`
	OTPBypassToken              string       `json:"otpBypassToken,omitempty"`
	Region                      MerakiRegion `json:"region,omitempty"`
	CollectorID                 string       `json:"collectorId,omitempty"`
	DisablePrivateAPICollection bool         `json:"disablePrivateApiCollection"`
}

// MarshalJSON writes the body with "type":"MERAKI", the property Forward picks the request's shape by.
func (t MerakiConnectivityTest) MarshalJSON() ([]byte, error) {
	type plain MerakiConnectivityTest
	return json.Marshal(struct {
		Type string `json:"type"`
		plain
	}{"MERAKI", plain(t)})
}

// MerakiSetupPatch is a partial update (MerakiSetupPatch, JsonProp semantics). A nil field is absent and leaves the stored value;
// a set field replaces it; the Clear flags send an explicit null, which resets the value.
//
// Setting DisablePrivateAPICollection to true also clears the stored CredentialID, which only the private APIs use; false (the
// same as Forward's null reset, which this patch never needs to send) turns private collection back on, which then needs a CredentialID. A Name different from
// the stored one (ignoring case) renames the setup. The patch is sent as type=MERAKI; Forward's guide says a setup's type cannot
// change once saved, but the route does not refuse a patch of another type's setup, so name a Meraki setup.
type MerakiSetupPatch struct {
	Name                        string            `json:"name,omitempty"`
	APIKeyID                    string            `json:"apiKeyId,omitempty"`
	CredentialID                string            `json:"credentialId,omitempty"`
	DisablePrivateAPICollection *bool             `json:"disablePrivateApiCollection,omitempty"`
	Region                      MerakiRegion      `json:"region,omitempty"`
	Collect                     *bool             `json:"collect,omitempty"`
	CollectorID                 string            `json:"collectorId,omitempty"`
	Hosts                       *[]string         `json:"hosts,omitempty"`
	Concurrency                 *int              `json:"concurrency,omitempty"`
	TestResult                  *MerakiTestResult `json:"testResult,omitempty"`

	// ClearCredential sends credentialId as null (no dashboard login).
	ClearCredential bool `json:"-"`
	// ClearCollector sends collectorId as null (the network's default collector).
	ClearCollector bool `json:"-"`
	// ClearConcurrency sends concurrency as null (Forward's default).
	ClearConcurrency bool `json:"-"`
}

// MarshalJSON writes the set properties, and a JSON null for each Clear flag.
func (p MerakiSetupPatch) MarshalJSON() ([]byte, error) {
	type plain MerakiSetupPatch
	switch {
	case p.ClearCredential && p.CredentialID != "":
		return nil, errors.New("forward: meraki setup patch both sets and clears credentialId")
	case p.ClearCollector && p.CollectorID != "":
		return nil, errors.New("forward: meraki setup patch both sets and clears collectorId")
	case p.ClearConcurrency && p.Concurrency != nil:
		return nil, errors.New("forward: meraki setup patch both sets and clears concurrency")
	case p.Concurrency != nil && (*p.Concurrency < 1 || *p.Concurrency > MerakiMaxConcurrency):
		return nil, errors.New("forward: meraki setup concurrency must be from 1 to 10 when set")
	}
	raw, err := json.Marshal(plain(p))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	// "type":"MERAKI" for the same reason as NewMerakiSetup.MarshalJSON: the patch classes are polymorphic on it too.
	fields["type"] = json.RawMessage(`"MERAKI"`)
	for name, clear := range map[string]bool{"credentialId": p.ClearCredential, "collectorId": p.ClearCollector, "concurrency": p.ClearConcurrency} {
		if clear {
			fields[name] = json.RawMessage("null")
		}
	}
	return json.Marshal(fields)
}
