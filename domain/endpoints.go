package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Endpoint is one network endpoint. Which fields apply depends on Type: Protocol, JumpServerID and LargeRTT are CLI-only, FullCollect is
// CLI and SNMP, DisableSSLValidation is HTTP-only (the published NewCli/NewSnmp/NewHttpNetworkEndpoint). AddBatch sends only the fields
// of the batch's type and refuses one set for another type.
type Endpoint struct {
	Type                 string `json:"type"`
	Name                 string `json:"name"`
	Host                 string `json:"host"`
	Port                 int    `json:"port,omitempty"`
	Protocol             string `json:"protocol,omitempty"`
	CredentialID         string `json:"credentialId,omitempty"`
	ProfileID            string `json:"profileId,omitempty"`
	JumpServerID         string `json:"jumpServerId,omitempty"`
	FullCollect          bool   `json:"fullCollectionLog,omitempty"`
	LargeRTT             bool   `json:"largeRtt,omitempty"`
	DisableSSLValidation *bool  `json:"disableSslValidation,omitempty"`
	Collect              *bool  `json:"collect,omitempty"`
	Note                 string `json:"note,omitempty"`
	// CollectorID pins the endpoint to one collector (collectorId on every New*NetworkEndpoint; hidden from the published spec, so Preview).
	// Forward writes it "C<n>" and accepts "C<n>" or "<n>". Empty sends nothing, so the network's default collector applies.
	CollectorID Identifier `json:"collectorId,omitempty"`
}

// EndpointPatch changes the stated parts of one endpoint; nil fields are left alone. Protocol and JumpServerID apply to CLI endpoints
// only, and Patch refuses them for another type.
type EndpointPatch struct {
	Host         *string `json:"host,omitempty"`
	Protocol     *string `json:"protocol,omitempty"`
	CredentialID *string `json:"credentialId,omitempty"`
	ProfileID    *string `json:"profileId,omitempty"`
	JumpServerID *string `json:"jumpServerId,omitempty"`
	Collect      *bool   `json:"collect,omitempty"`
	// CollectorID moves the endpoint to another collector; nil leaves it alone.
	CollectorID *string `json:"collectorId,omitempty"`
}

// EndpointProfile is one endpoint profile: how Forward recognizes and collects a kind of endpoint device. Type selects which fields
// apply -- CLI, SNMP or HTTP -- and ID always carries it as a prefix ("CLI-7", "SNMP-5", "HTTP-4"). The fields are the published
// CliEndpointProfileDef, SnmpEndpointProfileDef and HttpEndpointProfileDef, plus attribution; Raw keeps the whole object so a field this
// version does not model is not lost.
type EndpointProfile struct {
	ID   Identifier `json:"id,omitempty"`
	Name string     `json:"name"`
	Type string     `json:"type"`

	// CLI profiles. CLI commands run only if the organization has approved them (Endpoints.ApprovedCLICommands).
	CommandSets           []string `json:"commandSets,omitempty"`
	CustomCommands        []string `json:"customCommands,omitempty"`
	DetectorCommand       string   `json:"detectorCommand,omitempty"`
	DetectorErrorPatterns []string `json:"detectorErrorPatterns,omitempty"`
	NameDetectorCommand   string   `json:"nameDetectorCommand,omitempty"`
	Prompt                string   `json:"prompt,omitempty"`
	PromptResponse        string   `json:"promptResponse,omitempty"`
	PagePrompt            string   `json:"pagePrompt,omitempty"`
	PagePromptResponse    string   `json:"pagePromptResponse,omitempty"`
	PtyType               string   `json:"ptyType,omitempty"`
	PtyColumnSize         *int     `json:"ptyColumnSize,omitempty"`
	ClearPrompt           string   `json:"clearPrompt,omitempty"`

	// SNMP profiles.
	DetectorOID     string      `json:"detectorOid,omitempty"`
	NameDetectorOID string      `json:"nameDetectorOid,omitempty"`
	OIDSets         []string    `json:"oidSets,omitempty"`
	CustomOIDs      []CustomOID `json:"customOids,omitempty"`

	// HTTP profiles. AuthType is NONE or BASIC_AUTH.
	HTTPS       *bool             `json:"https,omitempty"`
	AuthType    string            `json:"authType,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	DetectorURI string            `json:"detectorUri,omitempty"`
	Endpoints   []HTTPEndpoint    `json:"endpoints,omitempty"`

	// Shared by two or more types.
	DetectorPatterns     []string `json:"detectorPatterns,omitempty"`
	NameDetectorPatterns []string `json:"nameDetectorPatterns,omitempty"`
	ResponseTimeoutSec   *int     `json:"responseTimeoutSec,omitempty"`

	CreatedAt   string     `json:"createdAt,omitempty"`
	CreatedBy   string     `json:"createdBy,omitempty"`
	CreatedByID Identifier `json:"createdById,omitempty"`
	UpdatedAt   string     `json:"updatedAt,omitempty"`
	UpdatedBy   string     `json:"updatedBy,omitempty"`
	UpdatedByID Identifier `json:"updatedById,omitempty"`

	Raw map[string]json.RawMessage `json:"-"`
}

// CustomOID is one extra OID an SNMP profile collects; Name names the file the value is stored in.
type CustomOID struct {
	Name string `json:"name"`
	OID  string `json:"oid"`
}

// HTTPEndpoint is one HTTP path an HTTP profile collects. PaginationModel stays raw: its shape varies by pagination type.
type HTTPEndpoint struct {
	Name            string          `json:"name"`
	Path            string          `json:"path"`
	PaginationModel json.RawMessage `json:"paginationModel,omitempty"`
}

// UnmarshalJSON decodes the modelled fields and keeps the whole object in Raw.
func (p *EndpointProfile) UnmarshalJSON(data []byte) error {
	type plain EndpointProfile
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = EndpointProfile(value)
	p.Raw = raw
	return nil
}

// ApprovedCLICommands is the organization's list of CLI commands endpoint profiles may run (ApprovedCliProfileCommands); a profile
// command outside it is not run. With no list uploaded, Forward returns its defaults and SignedAt and Uploaded* are empty.
type ApprovedCLICommands struct {
	Commands     []string   `json:"commands"`
	SignedAt     string     `json:"signedAt,omitempty"`
	UploadedAt   string     `json:"uploadedAt,omitempty"`
	UploadedBy   string     `json:"uploadedBy,omitempty"`
	UploadedByID Identifier `json:"uploadedById,omitempty"`
}

type EndpointProfileRequest struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	CustomCommands []string `json:"customCommands"`
	CommandSets    []string `json:"commandSets"`
}

type SourceTestStatus struct {
	Name string `json:"name,omitempty"`
	// Type is the connection type the device is configured with (empty when it is left to auto-discovery). For a classic device it
	// is a ConnTypes ID; Endpoints.ListTestStatuses fills it with the endpoint's own type (CLI, SNMP or HTTP), which is not.
	Type string `json:"type,omitempty"`
	// DiscoveredType is the type the last connectivity test discovered (testResult.discoveredType; empty when none was). It can
	// be "unknown" or a non-device type; treat those as absent and fall back to Type, as Forward does, before joining on ConnTypes.
	DiscoveredType string `json:"discoveredType,omitempty"`
	// ControllerInfos are the controllers the last connectivity test found for the device (testResult.controllerInfos), each
	// {name, ipAddress}. Non-empty means the device is managed through a controller.
	ControllerInfos   []ControllerInfo `json:"controllerInfos,omitempty"`
	SourceKind        string           `json:"sourceKind,omitempty"`
	TestResultPresent bool             `json:"testResultPresent,omitempty"`
	// TestStartTime, TestEndTime and TestSavedAt are when the STORED test result's test started, ended and was saved
	// (testResult.startTime, endTime, savedAt; ISO-8601 strings, empty when there is no test result). A result is only as
	// fresh as these: Forward keeps the last one until the next test replaces it.
	TestStartTime        string `json:"testStartTime,omitempty"`
	TestEndTime          string `json:"testEndTime,omitempty"`
	TestSavedAt          string `json:"testSavedAt,omitempty"`
	ConnectivityError    string `json:"connectivityError,omitempty"`
	ConnectivityErrorRaw string `json:"connectivityErrorRaw,omitempty"`
	ErrorPhase           string `json:"errorPhase,omitempty"`
	// SNMPCollectionStatus is the error type of the device's last SNMP collection when it failed (NO_RESPONSE, NOT_AUTHORIZED, ...),
	// and empty when it succeeded or was not reported.
	SNMPCollectionStatus string `json:"snmpCollectionStatus,omitempty"`
	// Forward reports snmpCollectionStatus only for a device with SNMP collection enabled, as {timestamp} after a successful collection
	// and {timestamp, errorType, error} after a failed one (SnmpCollectionStatus with SnmpErrorDetails unwrapped).
	// SNMPCollectionReported says the status was present at all, so "SNMP collection is off or has never run" is distinguishable from
	// "succeeded"; SNMPLastCollectedAt is the timestamp; SNMPCollectionErrorType (one of Forward's SnmpErrorType values) and
	// SNMPCollectionError (its message) are set only after a failure.
	SNMPCollectionReported  bool   `json:"snmpCollectionReported,omitempty"`
	SNMPLastCollectedAt     string `json:"snmpLastCollectedAt,omitempty"`
	SNMPCollectionErrorType string `json:"snmpCollectionErrorType,omitempty"`
	SNMPCollectionError     string `json:"snmpCollectionError,omitempty"`
}

// ControllerInfo is a controller's hostname and address (Forward's NameAndIpAddress); either may be empty.
type ControllerInfo struct {
	Name      string `json:"name,omitempty"`
	IPAddress string `json:"ipAddress,omitempty"`
}

// EndpointProfilePatch changes some of a profile's definition. Only the fields
// that are set are sent; the profile's type, taken from its ID (CLI-, SNMP- or
// HTTP-), decides which apply, and a field of another type is refused.
//
//   - CLI: Name, DetectorCommand, DetectorPatterns, DetectorErrorPatterns,
//     NameDetectorCommand, NameDetectorPatterns, Prompt, PromptResponse,
//     PagePrompt, PagePromptResponse, PtyType, PtyColumnSize, ClearPrompt,
//     ResponseTimeoutSec, CommandSets, CustomCommands.
//   - SNMP: Name, DetectorOID, DetectorPatterns, NameDetectorOID,
//     NameDetectorPatterns, ResponseTimeoutSec, OIDSets, CustomOIDs.
//   - HTTP: Name, HTTPS, AuthType, Headers, DetectorURI, DetectorPatterns,
//     Endpoints.
//
// A list or map replaces the whole value ("include every one you want to
// keep"), and an empty non-nil list is a real value. To remove a setting rather
// than change it, name its JSON key in Clear (for example "detectorCommand"):
// that sends an explicit null, which Forward accepts for the nullable
// settings and answers 400 for the rest. Forward validates the combined
// definition and returns 400 if it is not valid; a CLI profile's CommandSets
// and CustomCommands are subject to the organization's approved commands.
type EndpointProfilePatch struct {
	Name *string

	DetectorPatterns     []string
	NameDetectorPatterns []string
	ResponseTimeoutSec   *int

	// CLI.
	DetectorCommand       *string
	DetectorErrorPatterns []string
	NameDetectorCommand   *string
	Prompt                *string
	PromptResponse        *string
	PagePrompt            *string
	PagePromptResponse    *string
	PtyType               *string
	PtyColumnSize         *int
	ClearPrompt           *string
	CommandSets           []string
	CustomCommands        []string

	// SNMP.
	DetectorOID     *string
	NameDetectorOID *string
	OIDSets         []string
	CustomOIDs      []CustomOID

	// HTTP. HTTPS may not be cleared.
	HTTPS       *bool
	AuthType    *string
	Headers     map[string]string
	DetectorURI *string
	Endpoints   []HTTPEndpoint

	// Clear names the JSON keys to set to null.
	Clear []string
}

var endpointProfileFields = map[string]map[string]bool{
	"CLI": {"name": true, "detectorCommand": true, "detectorPatterns": true, "detectorErrorPatterns": true, "nameDetectorCommand": true,
		"nameDetectorPatterns": true, "prompt": true, "promptResponse": true, "pagePrompt": true, "pagePromptResponse": true, "ptyType": true,
		"ptyColumnSize": true, "clearPrompt": true, "responseTimeoutSec": true, "commandSets": true, "customCommands": true},
	"SNMP": {"name": true, "detectorOid": true, "detectorPatterns": true, "nameDetectorOid": true, "nameDetectorPatterns": true,
		"responseTimeoutSec": true, "oidSets": true, "customOids": true},
	"HTTP": {"name": true, "https": true, "authType": true, "headers": true, "detectorUri": true, "detectorPatterns": true, "endpoints": true},
}

// fields returns the patch as the JSON object to send, keyed by wire name.
func (p EndpointProfilePatch) fields() map[string]any {
	body := map[string]any{}
	str := func(key string, v *string) {
		if v != nil {
			body[key] = *v
		}
	}
	num := func(key string, v *int) {
		if v != nil {
			body[key] = *v
		}
	}
	list := func(key string, v []string) {
		if v != nil {
			body[key] = v
		}
	}
	str("name", p.Name)
	list("detectorPatterns", p.DetectorPatterns)
	list("nameDetectorPatterns", p.NameDetectorPatterns)
	num("responseTimeoutSec", p.ResponseTimeoutSec)
	str("detectorCommand", p.DetectorCommand)
	list("detectorErrorPatterns", p.DetectorErrorPatterns)
	str("nameDetectorCommand", p.NameDetectorCommand)
	str("prompt", p.Prompt)
	str("promptResponse", p.PromptResponse)
	str("pagePrompt", p.PagePrompt)
	str("pagePromptResponse", p.PagePromptResponse)
	str("ptyType", p.PtyType)
	num("ptyColumnSize", p.PtyColumnSize)
	str("clearPrompt", p.ClearPrompt)
	list("commandSets", p.CommandSets)
	list("customCommands", p.CustomCommands)
	str("detectorOid", p.DetectorOID)
	str("nameDetectorOid", p.NameDetectorOID)
	list("oidSets", p.OIDSets)
	if p.CustomOIDs != nil {
		body["customOids"] = p.CustomOIDs
	}
	if p.HTTPS != nil {
		body["https"] = *p.HTTPS
	}
	str("authType", p.AuthType)
	if p.Headers != nil {
		body["headers"] = p.Headers
	}
	str("detectorUri", p.DetectorURI)
	if p.Endpoints != nil {
		body["endpoints"] = p.Endpoints
	}
	return body
}

// Body validates the patch against the profile's type and returns the JSON.
func (p EndpointProfilePatch) Body(profileID string) ([]byte, error) {
	kind, _, found := strings.Cut(profileID, "-")
	allowed, ok := endpointProfileFields[kind]
	if !found || !ok {
		return nil, fmt.Errorf("forward: endpoint profile ID %q must start with CLI-, SNMP- or HTTP-", profileID)
	}
	body := p.fields()
	for key := range body {
		if !allowed[key] {
			return nil, fmt.Errorf("forward: %s is not a field of a %s endpoint profile", key, kind)
		}
	}
	for _, key := range p.Clear {
		if !allowed[key] {
			return nil, fmt.Errorf("forward: cannot clear %s on a %s endpoint profile", key, kind)
		}
		if key == "https" || key == "name" {
			return nil, fmt.Errorf("forward: %s cannot be cleared", key)
		}
		if _, set := body[key]; set {
			return nil, fmt.Errorf("forward: %s is both set and cleared", key)
		}
		body[key] = nil
	}
	if len(body) == 0 {
		return nil, errors.New("forward: an endpoint profile patch must change something")
	}
	return json.Marshal(body)
}

// CLICommandAssessment splits commands by whether the organization's approved list allows them.
type CLICommandAssessment struct {
	Approved   []string `json:"approved"`
	Unapproved []string `json:"unapproved"`
}
