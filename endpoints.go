package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type EndpointsService service

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
}

// endpointBody is the create body of one endpoint for endpointType. Forward binds each type to its own class
// (NewCli/NewSnmp/NewHttpNetworkEndpoint, an endpoint class unwrapped plus collect/collectorId/note) and rejects a field that class does
// not declare -- an SNMP endpoint sent "protocol" answers 400 'Unrecognized field "protocol" (class SnmpNetworkEndpoint)'. "type" is
// the NetworkEndpoint type discriminator, so it is always sent and must match.
func endpointBody(endpointType string, e Endpoint) (any, error) {
	if t := strings.ToUpper(strings.TrimSpace(e.Type)); t != "" && t != endpointType {
		return nil, fmt.Errorf("forward: endpoint %s has type %s in a %s batch", e.Name, e.Type, endpointType)
	}
	notFor := func(field string) error {
		return fmt.Errorf("forward: %s is not a field of a %s endpoint (%s)", field, endpointType, e.Name)
	}
	switch endpointType {
	case "CLI":
		if e.DisableSSLValidation != nil {
			return nil, notFor("disableSslValidation")
		}
		return struct {
			Type         string `json:"type"`
			Name         string `json:"name"`
			Host         string `json:"host"`
			Port         int    `json:"port,omitempty"`
			Protocol     string `json:"protocol,omitempty"`
			ProfileID    string `json:"profileId,omitempty"`
			CredentialID string `json:"credentialId,omitempty"`
			JumpServerID string `json:"jumpServerId,omitempty"`
			FullCollect  bool   `json:"fullCollectionLog,omitempty"`
			LargeRTT     bool   `json:"largeRtt,omitempty"`
			Collect      *bool  `json:"collect,omitempty"`
			Note         string `json:"note,omitempty"`
		}{endpointType, e.Name, e.Host, e.Port, e.Protocol, e.ProfileID, e.CredentialID, e.JumpServerID, e.FullCollect, e.LargeRTT, e.Collect, e.Note}, nil
	case "SNMP", "HTTP":
		switch {
		case e.Protocol != "":
			return nil, notFor("protocol")
		case e.JumpServerID != "":
			return nil, notFor("jumpServerId")
		case e.LargeRTT:
			return nil, notFor("largeRtt")
		}
		if endpointType == "SNMP" {
			if e.DisableSSLValidation != nil {
				return nil, notFor("disableSslValidation")
			}
			return struct {
				Type         string `json:"type"`
				Name         string `json:"name"`
				Host         string `json:"host"`
				Port         int    `json:"port,omitempty"`
				ProfileID    string `json:"profileId,omitempty"`
				CredentialID string `json:"credentialId,omitempty"`
				FullCollect  bool   `json:"fullCollectionLog,omitempty"`
				Collect      *bool  `json:"collect,omitempty"`
				Note         string `json:"note,omitempty"`
			}{endpointType, e.Name, e.Host, e.Port, e.ProfileID, e.CredentialID, e.FullCollect, e.Collect, e.Note}, nil
		}
		if e.FullCollect {
			return nil, notFor("fullCollectionLog")
		}
		return struct {
			Type                 string `json:"type"`
			Name                 string `json:"name"`
			Host                 string `json:"host"`
			Port                 int    `json:"port,omitempty"`
			ProfileID            string `json:"profileId,omitempty"`
			CredentialID         string `json:"credentialId,omitempty"`
			DisableSSLValidation *bool  `json:"disableSslValidation,omitempty"`
			Collect              *bool  `json:"collect,omitempty"`
			Note                 string `json:"note,omitempty"`
		}{endpointType, e.Name, e.Host, e.Port, e.ProfileID, e.CredentialID, e.DisableSSLValidation, e.Collect, e.Note}, nil
	default:
		return nil, fmt.Errorf("forward: endpoint type must be CLI, SNMP or HTTP, not %q", endpointType)
	}
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
	endpointType = strings.ToUpper(strings.TrimSpace(endpointType))
	if endpointType == "" {
		endpointType = "CLI"
	}
	bodies := make([]any, 0, len(endpoints))
	for _, endpoint := range endpoints {
		body, err := endpointBody(endpointType, endpoint)
		if err != nil {
			return nil, err
		}
		bodies = append(bodies, body)
	}
	query := url.Values{"action": []string{"addBatch"}, "type": []string{endpointType}}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?"+query.Encode(), bodies)
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
	endpointType = strings.ToUpper(strings.TrimSpace(endpointType))
	if endpointType == "" {
		endpointType = "CLI"
	}
	// Cli/Snmp/HttpNetworkEndpointPatch: protocol and jumpServerId exist on the CLI patch only.
	if endpointType != "CLI" && (patch.Protocol != nil || patch.JumpServerID != nil) {
		return nil, fmt.Errorf("forward: protocol and jumpServerId are not fields of a %s endpoint", endpointType)
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
	if isGone(err) {
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
	if isGone(err) {
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

// body validates the patch against the profile's type and returns the JSON.
func (p EndpointProfilePatch) body(profileID string) ([]byte, error) {
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

// UpdateProfile changes an endpoint profile in place and returns the stored
// profile. PATCH /api/endpoint-profiles/{profileId}, one route per type
// (updateCli/Snmp/HttpEndpointProfile; MANAGE_ENDPOINT_PROFILES). The change
// applies to every endpoint using the profile from its next collection, so
// check Endpoints.List for what uses it first. Forward answers 404 for an
// unknown profile. All three routes are published.
func (s *EndpointsService) UpdateProfile(ctx context.Context, profileID string, patch EndpointProfilePatch) (*EndpointProfile, *Response, error) {
	profileID = strings.TrimSpace(profileID)
	body, err := patch.body(profileID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/endpoint-profiles/"+url.PathEscape(profileID), json.RawMessage(body))
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.UpdateProfile")
	out := new(EndpointProfile)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// CLICommandAssessment splits commands by whether the organization's approved list allows them.
type CLICommandAssessment struct {
	Approved   []string `json:"approved"`
	Unapproved []string `json:"unapproved"`
}

// AssessCLICommands says which of commands the organization's approved list allows. Forward matches each command against
// every approved regex with find(), not a full match, and an endpoint profile is collected only if all of its commands
// are approved. Nothing is stored. POST /api/approved-cli-commands?action=assess, JSON list of strings
// (CliCommandsController.assessCliCommands; MANAGE_ENDPOINT_PROFILES, although nothing is written; on primary
// 15398425a69 and stable 67e89c87124). Preview: not in the published spec. An empty list is answered without a request.
func (s *EndpointsService) AssessCLICommands(ctx context.Context, commands []string) (*CLICommandAssessment, *Response, error) {
	if len(commands) == 0 {
		return &CLICommandAssessment{Approved: []string{}, Unapproved: []string{}}, nil, nil
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/approved-cli-commands?action=assess", commands)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.AssessCLICommands")
	out := new(CLICommandAssessment)
	response, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, response, err
	}
	if out.Approved == nil {
		out.Approved = []string{}
	}
	if out.Unapproved == nil {
		out.Unapproved = []string{}
	}
	return out, response, nil
}

// UpdateApprovedCLICommands replaces the organization's approved CLI command list with a Forward-signed file; a list
// cannot be authored locally, because Forward verifies the signature and that the identifiers match the organization
// and answers 400 ("Signature mismatch" or "Identifier mismatch") otherwise. The file is sent as is. This changes which
// endpoint profiles are collected. POST /api/approved-cli-commands?action=update, multipart "file"
// (CliCommandsController.updateApprovedCliCommands; MANAGE_ENDPOINT_PROFILES; on primary 15398425a69 and stable
// 67e89c87124). Preview: not in the published spec.
func (s *EndpointsService) UpdateApprovedCLICommands(ctx context.Context, fileName string, signed []byte) (*ApprovedCLICommands, *Response, error) {
	if len(signed) == 0 {
		return nil, nil, errors.New("forward: the signed approved-commands file is empty")
	}
	body, contentType, err := dataFileMultipart(func(w *multipart.Writer) error {
		return writeDataFilePart(w, fileName, signed)
	})
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/approved-cli-commands?action=update", body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req = markOperation(req, "Endpoints.UpdateApprovedCLICommands")
	out := new(ApprovedCLICommands)
	response, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, response, err
	}
	return out, response, nil
}
