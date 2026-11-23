package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Endpoints service

// endpointBody is the create body of one endpoint for endpointType. Forward binds each type to its own class
// (NewCli/NewSnmp/NewHttpNetworkEndpoint, an endpoint class unwrapped plus collect/collectorId/note) and rejects a field that class does
// not declare -- an SNMP endpoint sent "protocol" answers 400 'Unrecognized field "protocol" (class SnmpNetworkEndpoint)'. "type" is
// the NetworkEndpoint type discriminator, so it is always sent and must match.
func endpointBody(endpointType string, e ports.Endpoint) (any, error) {
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
			CollectorID  string `json:"collectorId,omitempty"`
			Note         string `json:"note,omitempty"`
		}{endpointType, e.Name, e.Host, e.Port, e.Protocol, e.ProfileID, e.CredentialID, e.JumpServerID, e.FullCollect, e.LargeRTT, e.Collect, string(e.CollectorID), e.Note}, nil
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
				CollectorID  string `json:"collectorId,omitempty"`
				Note         string `json:"note,omitempty"`
			}{endpointType, e.Name, e.Host, e.Port, e.ProfileID, e.CredentialID, e.FullCollect, e.Collect, string(e.CollectorID), e.Note}, nil
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
			CollectorID          string `json:"collectorId,omitempty"`
			Note                 string `json:"note,omitempty"`
		}{endpointType, e.Name, e.Host, e.Port, e.ProfileID, e.CredentialID, e.DisableSSLValidation, e.Collect, string(e.CollectorID), e.Note}, nil
	default:
		return nil, fmt.Errorf("forward: endpoint type must be CLI, SNMP or HTTP, not %q", endpointType)
	}
}

func (s *Endpoints) List(ctx context.Context, networkID string) ([]ports.Endpoint, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.networkPath(networkID, "/endpoints")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.Endpoint]{Keys: []string{"items", "endpoints", "data", "results"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *Endpoints) AddBatch(ctx context.Context, networkID, endpointType string, endpoints []ports.Endpoint) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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

func (s *Endpoints) Patch(ctx context.Context, networkID, name, endpointType string, patch ports.EndpointPatch) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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

func (s *Endpoints) ListTestStatuses(ctx context.Context, networkID string) ([]ports.SourceTestStatus, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.networkPath(networkID, "/endpoints?with=testResult")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.SourceTestStatus]{Keys: []string{"endpoints", "items", "data", "results"}, AllowSingle: true}
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

func (s *Endpoints) ListProfiles(ctx context.Context, profileType string) ([]ports.EndpointProfile, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path := "/api/endpoint-profiles"
	if profileType = strings.TrimSpace(profileType); profileType != "" {
		path += "?type=" + url.QueryEscape(profileType)
	}
	result := listResponse[ports.EndpointProfile]{Keys: []string{"items", "profiles", "endpointProfiles", "data", "results"}, AllowSingle: true}
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
func (s *Endpoints) GetProfile(ctx context.Context, profileID string) (*ports.EndpointProfile, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if profileID = strings.TrimSpace(profileID); profileID == "" {
		return nil, nil, errors.New("forward: endpoint profile ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/endpoint-profiles/"+url.PathEscape(profileID), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.GetProfile")
	out := new(ports.EndpointProfile)
	response, err := s.client.doRequired(req, out)
	if ports.IsStatus(err, http.StatusNotFound) {
		return nil, response, nil
	}
	if err != nil {
		return nil, response, err
	}
	return out, response, nil
}

// ApprovedCLICommands returns the CLI commands the organization allows endpoint profiles to run. GET /api/approved-cli-commands
// (CliCommandsController; VIEW_ENDPOINT_PROFILES; on primary 15398425a69 and stable 67e89c87124). Preview: not in the published spec.
func (s *Endpoints) ApprovedCLICommands(ctx context.Context) (*ports.ApprovedCLICommands, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/approved-cli-commands", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.ApprovedCLICommands")
	out := new(ports.ApprovedCLICommands)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

func (s *Endpoints) CreateProfile(ctx context.Context, input ports.EndpointProfileRequest) (*ports.EndpointProfile, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.EndpointProfile)
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
func (s *Endpoints) CreateProfileDefinition(ctx context.Context, def ports.EndpointProfile) (*ports.EndpointProfile, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.EndpointProfile)
	response, err := s.client.doRequired(req, out)
	if err == nil && out.ID == "" {
		err = errors.New("forward: endpoint profile create returned no ID")
	}
	return out, response, err
}

// profileDefinitionBody keeps only the published fields of def's type, so an SNMP profile never carries CLI or HTTP keys.
func profileDefinitionBody(def ports.EndpointProfile) (any, error) {
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
			Type                 string            `json:"type"`
			Name                 string            `json:"name"`
			DetectorOID          string            `json:"detectorOid,omitempty"`
			DetectorPatterns     []string          `json:"detectorPatterns,omitempty"`
			NameDetectorOID      string            `json:"nameDetectorOid,omitempty"`
			NameDetectorPatterns []string          `json:"nameDetectorPatterns,omitempty"`
			ResponseTimeoutSec   *int              `json:"responseTimeoutSec,omitempty"`
			OIDSets              []string          `json:"oidSets,omitempty"`
			CustomOIDs           []ports.CustomOID `json:"customOids,omitempty"`
		}{def.Type, def.Name, def.DetectorOID, def.DetectorPatterns, def.NameDetectorOID, def.NameDetectorPatterns,
			def.ResponseTimeoutSec, def.OIDSets, def.CustomOIDs}, nil
	case "HTTP":
		if def.HTTPS == nil || def.AuthType == "" || len(def.Endpoints) == 0 {
			return nil, errors.New("forward: an HTTP endpoint profile requires HTTPS, AuthType and at least one endpoint")
		}
		return struct {
			Type             string               `json:"type"`
			Name             string               `json:"name"`
			HTTPS            bool                 `json:"https"`
			AuthType         string               `json:"authType"`
			Headers          map[string]string    `json:"headers,omitempty"`
			DetectorURI      string               `json:"detectorUri,omitempty"`
			DetectorPatterns []string             `json:"detectorPatterns,omitempty"`
			Endpoints        []ports.HTTPEndpoint `json:"endpoints"`
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
func (s *Endpoints) DeleteProfile(ctx context.Context, profileID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if profileID = strings.TrimSpace(profileID); profileID == "" {
		return nil, errors.New("forward: endpoint profile ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/endpoint-profiles/"+url.PathEscape(profileID), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Endpoints.DeleteProfile")
	response, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
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
func (s *Endpoints) Delete(ctx context.Context, networkID, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
	if ports.IsGone(err) {
		return response, nil
	}
	return response, err
}

func (s *Endpoints) networkPath(networkID, suffix string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + suffix, nil
}

// UpdateProfile changes an endpoint profile in place and returns the stored
// profile. PATCH /api/endpoint-profiles/{profileId}, one route per type
// (updateCli/Snmp/HttpEndpointProfile; MANAGE_ENDPOINT_PROFILES). The change
// applies to every endpoint using the profile from its next collection, so
// check Endpoints.List for what uses it first. Forward answers 404 for an
// unknown profile. All three routes are published.
func (s *Endpoints) UpdateProfile(ctx context.Context, profileID string, patch ports.EndpointProfilePatch) (*ports.EndpointProfile, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	profileID = strings.TrimSpace(profileID)
	body, err := patch.Body(profileID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/endpoint-profiles/"+url.PathEscape(profileID), json.RawMessage(body))
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.UpdateProfile")
	out := new(ports.EndpointProfile)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// AssessCLICommands says which of commands the organization's approved list allows. Forward matches each command against
// every approved regex with find(), not a full match, and an endpoint profile is collected only if all of its commands
// are approved. Nothing is stored. POST /api/approved-cli-commands?action=assess, JSON list of strings
// (CliCommandsController.assessCliCommands; MANAGE_ENDPOINT_PROFILES, although nothing is written; on primary
// 15398425a69 and stable 67e89c87124). Preview: not in the published spec. An empty list is answered without a request.
func (s *Endpoints) AssessCLICommands(ctx context.Context, commands []string) (*ports.CLICommandAssessment, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if len(commands) == 0 {
		return &ports.CLICommandAssessment{Approved: []string{}, Unapproved: []string{}}, nil, nil
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/approved-cli-commands?action=assess", commands)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.AssessCLICommands")
	out := new(ports.CLICommandAssessment)
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
func (s *Endpoints) UpdateApprovedCLICommands(ctx context.Context, fileName string, signed []byte) (*ports.ApprovedCLICommands, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.ApprovedCLICommands)
	response, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, response, err
	}
	return out, response, nil
}
