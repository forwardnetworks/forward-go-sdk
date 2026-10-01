package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type EndpointsService service

type Endpoint struct {
	Type         string `json:"type"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port,omitempty"`
	Protocol     string `json:"protocol"`
	CredentialID string `json:"credentialId,omitempty"`
	ProfileID    string `json:"profileId,omitempty"`
	JumpServerID string `json:"jumpServerId,omitempty"`
	FullCollect  bool   `json:"fullCollectionLog,omitempty"`
	LargeRTT     bool   `json:"largeRtt,omitempty"`
	Collect      *bool  `json:"collect,omitempty"`
	Note         string `json:"note,omitempty"`
}

type EndpointPatch struct {
	Host         *string `json:"host,omitempty"`
	Protocol     *string `json:"protocol,omitempty"`
	CredentialID *string `json:"credentialId,omitempty"`
	ProfileID    *string `json:"profileId,omitempty"`
	JumpServerID *string `json:"jumpServerId,omitempty"`
	Collect      *bool   `json:"collect,omitempty"`
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
	Name                 string `json:"name,omitempty"`
	SourceKind           string `json:"sourceKind,omitempty"`
	TestResultPresent    bool   `json:"testResultPresent,omitempty"`
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

func (s *EndpointsService) List(ctx context.Context, networkID string) ([]Endpoint, *Response, error) {
	path, err := s.networkPath(networkID, "/endpoints")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[Endpoint]{Keys: []string{"items", "endpoints", "data", "results"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *EndpointsService) AddBatch(ctx context.Context, networkID, endpointType string, endpoints []Endpoint) (*Response, error) {
	if len(endpoints) == 0 {
		return nil, nil
	}
	path, err := s.networkPath(networkID, "/endpoints")
	if err != nil {
		return nil, err
	}
	endpointType = strings.TrimSpace(endpointType)
	if endpointType == "" {
		endpointType = "CLI"
	}
	query := url.Values{"action": []string{"addBatch"}, "type": []string{endpointType}}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?"+query.Encode(), endpoints)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Endpoints.AddBatch")
	return s.client.Do(req, nil)
}

func (s *EndpointsService) Patch(ctx context.Context, networkID, name, endpointType string, patch EndpointPatch) (*Response, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("forward: endpoint name is required")
	}
	path, err := s.networkPath(networkID, "/endpoints/"+url.PathEscape(name))
	if err != nil {
		return nil, err
	}
	endpointType = strings.TrimSpace(endpointType)
	if endpointType == "" {
		endpointType = "CLI"
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path+"?type="+url.QueryEscape(endpointType), patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Endpoints.Patch")
	return s.client.Do(req, nil)
}

func (s *EndpointsService) ListTestStatuses(ctx context.Context, networkID string) ([]SourceTestStatus, *Response, error) {
	path, err := s.networkPath(networkID, "/endpoints?with=testResult")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[SourceTestStatus]{Keys: []string{"endpoints", "items", "data", "results"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.ListTestStatuses")
	response, err := s.client.doRequired(req, &result)
	for i := range result.Items {
		result.Items[i].SourceKind = "endpoint"
	}
	return result.Items, response, err
}

func (s *EndpointsService) ListProfiles(ctx context.Context, profileType string) ([]EndpointProfile, *Response, error) {
	path := "/api/endpoint-profiles"
	if profileType = strings.TrimSpace(profileType); profileType != "" {
		path += "?type=" + url.QueryEscape(profileType)
	}
	result := listResponse[EndpointProfile]{Keys: []string{"items", "profiles", "endpointProfiles", "data", "results"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.ListProfiles")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// GetProfile returns one endpoint profile by its full ID ("CLI-7"), or (nil, nil) when it does not exist. GET
// /api/endpoint-profiles/{profileId} (published getEndpointProfile; VIEW_ENDPOINT_PROFILES).
func (s *EndpointsService) GetProfile(ctx context.Context, profileID string) (*EndpointProfile, *Response, error) {
	if profileID = strings.TrimSpace(profileID); profileID == "" {
		return nil, nil, errors.New("forward: endpoint profile ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/endpoint-profiles/"+url.PathEscape(profileID), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.GetProfile")
	out := new(EndpointProfile)
	response, err := s.client.doRequired(req, out)
	if isStatus(err, http.StatusNotFound) {
		return nil, response, nil
	}
	if err != nil {
		return nil, response, err
	}
	return out, response, nil
}

// ApprovedCLICommands returns the CLI commands the organization allows endpoint profiles to run. GET /api/approved-cli-commands
// (CliCommandsController; VIEW_ENDPOINT_PROFILES; on primary 15398425a69 and stable 67e89c87124). Preview: not in the published spec.
func (s *EndpointsService) ApprovedCLICommands(ctx context.Context) (*ApprovedCLICommands, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/approved-cli-commands", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.ApprovedCLICommands")
	out := new(ApprovedCLICommands)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

func (s *EndpointsService) CreateProfile(ctx context.Context, input EndpointProfileRequest) (*EndpointProfile, *Response, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Type = strings.TrimSpace(input.Type)
	if input.Name == "" || input.Type == "" {
		return nil, nil, errors.New("forward: endpoint profile name and type are required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/endpoint-profiles?type="+url.QueryEscape(input.Type), input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.CreateProfile")
	out := new(EndpointProfile)
	response, err := s.client.doRequired(req, out)
	if err == nil && out.ID == "" {
		err = errors.New("forward: endpoint profile create returned no ID")
	}
	return out, response, err
}

// CreateProfileDefinition creates an endpoint profile of any type from def -- the read type, so a profile read back can be copied -- and
// returns it as Forward stored it, with its new ID ("SNMP-5"). Only the fields of def.Type are sent (CLI, SNMP or HTTP, per the published
// CliEndpointProfileDef / SnmpEndpointProfileDef / HttpEndpointProfileDef); ID, attribution and Raw are never sent. POST
// /api/endpoint-profiles?type= (createCli/Snmp/HttpEndpointProfile; 201 with the stored profile; MANAGE_ENDPOINT_PROFILES). A CLI
// profile's commands run only if the organization has approved them (ApprovedCLICommands).
func (s *EndpointsService) CreateProfileDefinition(ctx context.Context, def EndpointProfile) (*EndpointProfile, *Response, error) {
	def.Name, def.Type = strings.TrimSpace(def.Name), strings.ToUpper(strings.TrimSpace(def.Type))
	if def.Name == "" {
		return nil, nil, errors.New("forward: endpoint profile name is required")
	}
	body, err := profileDefinitionBody(def)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/endpoint-profiles?"+url.Values{"type": []string{def.Type}}.Encode(), body)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.CreateProfileDefinition")
	out := new(EndpointProfile)
	response, err := s.client.doRequired(req, out)
	if err == nil && out.ID == "" {
		err = errors.New("forward: endpoint profile create returned no ID")
	}
	return out, response, err
}

// profileDefinitionBody keeps only the published fields of def's type, so an SNMP profile never carries CLI or HTTP keys.
func profileDefinitionBody(def EndpointProfile) (any, error) {
	switch def.Type {
	case "CLI":
		return struct {
			Type                  string   `json:"type"`
			Name                  string   `json:"name"`
			DetectorCommand       string   `json:"detectorCommand,omitempty"`
			DetectorPatterns      []string `json:"detectorPatterns,omitempty"`
			DetectorErrorPatterns []string `json:"detectorErrorPatterns,omitempty"`
			NameDetectorCommand   string   `json:"nameDetectorCommand,omitempty"`
			NameDetectorPatterns  []string `json:"nameDetectorPatterns,omitempty"`
			Prompt                string   `json:"prompt,omitempty"`
			PromptResponse        string   `json:"promptResponse,omitempty"`
			PagePrompt            string   `json:"pagePrompt,omitempty"`
			PagePromptResponse    string   `json:"pagePromptResponse,omitempty"`
			PtyType               string   `json:"ptyType,omitempty"`
			PtyColumnSize         *int     `json:"ptyColumnSize,omitempty"`
			ClearPrompt           string   `json:"clearPrompt,omitempty"`
			ResponseTimeoutSec    *int     `json:"responseTimeoutSec,omitempty"`
			CommandSets           []string `json:"commandSets,omitempty"`
			CustomCommands        []string `json:"customCommands,omitempty"`
		}{def.Type, def.Name, def.DetectorCommand, def.DetectorPatterns, def.DetectorErrorPatterns, def.NameDetectorCommand,
			def.NameDetectorPatterns, def.Prompt, def.PromptResponse, def.PagePrompt, def.PagePromptResponse, def.PtyType,
			def.PtyColumnSize, def.ClearPrompt, def.ResponseTimeoutSec, def.CommandSets, def.CustomCommands}, nil
	case "SNMP":
		return struct {
			Type                 string      `json:"type"`
			Name                 string      `json:"name"`
			DetectorOID          string      `json:"detectorOid,omitempty"`
			DetectorPatterns     []string    `json:"detectorPatterns,omitempty"`
			NameDetectorOID      string      `json:"nameDetectorOid,omitempty"`
			NameDetectorPatterns []string    `json:"nameDetectorPatterns,omitempty"`
			ResponseTimeoutSec   *int        `json:"responseTimeoutSec,omitempty"`
			OIDSets              []string    `json:"oidSets,omitempty"`
			CustomOIDs           []CustomOID `json:"customOids,omitempty"`
		}{def.Type, def.Name, def.DetectorOID, def.DetectorPatterns, def.NameDetectorOID, def.NameDetectorPatterns,
			def.ResponseTimeoutSec, def.OIDSets, def.CustomOIDs}, nil
	case "HTTP":
		if def.HTTPS == nil || def.AuthType == "" || len(def.Endpoints) == 0 {
			return nil, errors.New("forward: an HTTP endpoint profile requires HTTPS, AuthType and at least one endpoint")
		}
		return struct {
			Type             string            `json:"type"`
			Name             string            `json:"name"`
			HTTPS            bool              `json:"https"`
			AuthType         string            `json:"authType"`
			Headers          map[string]string `json:"headers,omitempty"`
			DetectorURI      string            `json:"detectorUri,omitempty"`
			DetectorPatterns []string          `json:"detectorPatterns,omitempty"`
			Endpoints        []HTTPEndpoint    `json:"endpoints"`
		}{def.Type, def.Name, *def.HTTPS, def.AuthType, def.Headers, def.DetectorURI, def.DetectorPatterns, def.Endpoints}, nil
	default:
		return nil, errors.New("forward: endpoint profile type must be CLI, SNMP or HTTP, not " + strconv.Quote(def.Type))
	}
}

// DeleteProfile deletes an endpoint profile by its full ID. A 404 is success (already gone). Forward REFUSES to delete a profile that any
// endpoint in any of the organization's networks still uses -- a 400 "Profile is still used in N network(s)", ErrEndpointProfileInUse --
// rather than orphaning the endpoints, so an undo reassigns those endpoints first (Endpoints.Patch) and deletes after. Deleting also drops
// the profile's stored connectivity results. DELETE /api/endpoint-profiles/{profileId} (published deleteEndpointProfile;
// NetworkEndpointService.deleteProfile; MANAGE_ENDPOINT_PROFILES).
func (s *EndpointsService) DeleteProfile(ctx context.Context, profileID string) (*Response, error) {
	if profileID = strings.TrimSpace(profileID); profileID == "" {
		return nil, errors.New("forward: endpoint profile ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/endpoint-profiles/"+url.PathEscape(profileID), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Endpoints.DeleteProfile")
	response, err := s.client.Do(req, nil)
	if isStatus(err, http.StatusNotFound) {
		return response, nil
	}
	return response, err
}

// Delete removes one endpoint from a network.
//
// WHY THIS EXISTS. Forward's device-name namespace is SHARED between endpoints
// and classic devices. A name left behind as an endpoint after the device was
// reclassified as classic is a stale record of an earlier sync, and it
// collides with the classic putBatch forever until it is removed. Deleting it
// is therefore part of converging inventory, not a cleanup nicety.
//
// A 404 is SUCCESS: the desired state, that the endpoint is absent, already
// holds. This matches DeleteGroup and DeleteDeviceAccessLabel, and it is what
// makes the call safe to repeat.
func (s *EndpointsService) Delete(ctx context.Context, networkID, name string) (*Response, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("forward: endpoint name is required")
	}
	path, err := s.networkPath(networkID, "/endpoints/"+url.PathEscape(name))
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Endpoints.Delete")
	response, err := s.client.Do(req, nil)
	if isStatus(err, http.StatusNotFound) {
		return response, nil
	}
	return response, err
}

func (s *EndpointsService) networkPath(networkID, suffix string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + suffix, nil
}
