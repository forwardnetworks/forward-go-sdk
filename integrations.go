package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type IntegrationsService service

type InfobloxInstance struct {
	ID        Identifier `json:"id,omitempty"`
	Name      string     `json:"name"`
	IPAddress string     `json:"ipAddress"`
	Username  string     `json:"username,omitempty"`
}
type InfobloxInstanceRequest struct {
	Name      string `json:"name"`
	IPAddress string `json:"ipAddress"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

type Rapid7Source struct {
	ID                   Identifier `json:"id,omitempty"`
	Name                 string     `json:"name"`
	Type                 string     `json:"type,omitempty"`
	BaseURL              string     `json:"baseUrl,omitempty"`
	CredentialID         Identifier `json:"credentialId,omitempty"`
	CollectionDisabled   bool       `json:"collectionDisabled,omitempty"`
	DisableSSLValidation bool       `json:"disableSslValidation,omitempty"`
	ReportNames          []string   `json:"reportNames,omitempty"`
}
type Rapid7SourceRequest struct {
	Name                 string     `json:"name"`
	BaseURL              string     `json:"baseUrl"`
	DisableSSLValidation bool       `json:"disableSslValidation"`
	CredentialID         Identifier `json:"credentialId"`
	CollectionDisabled   bool       `json:"collectionDisabled"`
	ReportNames          []string   `json:"reportNames,omitempty"`
}

type ServiceNowIntegrationSettings struct {
	InstanceURL       string `json:"instanceUrl,omitempty"`
	Username          string `json:"username,omitempty"`
	Enabled           bool   `json:"enabled,omitempty"`
	AutoCreate        bool   `json:"autoCreate,omitempty"`
	AutoCreateImpact  string `json:"autoCreateImpact,omitempty"`
	AutoCreateUrgency string `json:"autoCreateUrgency,omitempty"`
	AutoUpdate        bool   `json:"autoUpdate,omitempty"`
}
type ServiceNowIntegrationRequest struct {
	InstanceURL       string `json:"instanceUrl,omitempty"`
	Username          string `json:"username"`
	Password          string `json:"password"`
	Enabled           bool   `json:"enabled"`
	AutoCreate        bool   `json:"autoCreate"`
	AutoCreateImpact  string `json:"autoCreateImpact"`
	AutoCreateUrgency string `json:"autoCreateUrgency"`
	AutoUpdate        bool   `json:"autoUpdate"`
}

type ServiceNowConnectionDetails struct {
	InstanceURL string `json:"instanceUrl"`
	Username    string `json:"username"`
	Password    string `json:"password"`
}
type ServiceNowCMDBField struct {
	Label     string `json:"label"`
	Element   string `json:"element"`
	DataType  string `json:"dataType"`
	Mandatory bool   `json:"mandatory"`
	MaxLength int    `json:"maxLength,omitempty"`
	Reference string `json:"reference,omitempty"`
}
type ServiceNowCMDBFieldRelation struct {
	CMDBField       ServiceNowCMDBField `json:"cmdbField"`
	QueryColumnName string              `json:"queryColumnName,omitempty"`
}
type ServiceNowCMDBSchema struct {
	Name   string                `json:"name"`
	Fields []ServiceNowCMDBField `json:"fields"`
}
type ServiceNowCMDBSchemaRequest struct {
	ConnectionDetails ServiceNowConnectionDetails `json:"connectionDetails"`
	ClassNames        []string                    `json:"classNames"`
}
type ServiceNowCMDBMapping struct {
	Name           string                        `json:"name"`
	QueryID        Identifier                    `json:"queryId"`
	FieldRelations []ServiceNowCMDBFieldRelation `json:"fieldRelations"`
}
type ServiceNowCMDBConfiguration struct {
	Settings   ServiceNowCMDBSettings   `json:"settings"`
	Mappings   []ServiceNowCMDBMapping  `json:"mappings"`
	SyncConfig ServiceNowCMDBSyncConfig `json:"syncConfig"`
	Enabled    bool                     `json:"enabled"`
}
type ServiceNowCMDBSettings struct {
	ConnectionName    string                      `json:"connectionName"`
	ConnectionDetails ServiceNowConnectionDetails `json:"connectionDetails"`
	SourceName        string                      `json:"sourceName"`
}
type ServiceNowCMDBSyncConfig struct {
	NetworkIDs []string `json:"networkIds"`
}

func (s *IntegrationsService) ListInfoblox(ctx context.Context) ([]InfobloxInstance, *Response, error) {
	return s.listInfoblox(ctx, "/api/integrations/infoblox/instances", "Integrations.ListInfoblox")
}
func (s *IntegrationsService) ListInfobloxLegacy(ctx context.Context) ([]InfobloxInstance, *Response, error) {
	return s.listInfoblox(ctx, "/api/integrations/infoblox", "Integrations.ListInfobloxLegacy")
}
func (s *IntegrationsService) listInfoblox(ctx context.Context, path, operation string) ([]InfobloxInstance, *Response, error) {
	result := listResponse[InfobloxInstance]{Keys: []string{"instances", "items", "data"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}
func (s *IntegrationsService) CreateInfoblox(ctx context.Context, input InfobloxInstanceRequest) (*InfobloxInstance, *Response, error) {
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/integrations/infoblox/instances", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Integrations.CreateInfoblox")
	out := new(InfobloxInstance)
	response, err := s.client.Do(req, out)
	if err == nil && strings.TrimSpace(out.Name) == "" {
		out.Name, out.IPAddress, out.Username = input.Name, input.IPAddress, input.Username
	}
	return out, response, err
}

func (s *IntegrationsService) ListRapid7(ctx context.Context, networkID string) ([]Rapid7Source, *Response, error) {
	path, err := integrationNetworkPath(s.client, networkID, "/end-host-scanners")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[Rapid7Source]{Keys: []string{"sources", "scanners", "items", "data"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Integrations.ListRapid7")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}
func (s *IntegrationsService) CreateRapid7(ctx context.Context, networkID string, input Rapid7SourceRequest) (*Rapid7Source, *Response, error) {
	return s.writeRapid7(ctx, http.MethodPost, networkID, "", input, "Integrations.CreateRapid7")
}
func (s *IntegrationsService) UpdateRapid7(ctx context.Context, networkID, name string, input Rapid7SourceRequest) (*Rapid7Source, *Response, error) {
	return s.writeRapid7(ctx, http.MethodPatch, networkID, name, input, "Integrations.UpdateRapid7")
}
func (s *IntegrationsService) writeRapid7(ctx context.Context, method, networkID, name string, input Rapid7SourceRequest, operation string) (*Rapid7Source, *Response, error) {
	path, err := integrationNetworkPath(s.client, networkID, "/rapid7-sources")
	if err != nil {
		return nil, nil, err
	}
	if name = strings.TrimSpace(name); name != "" {
		path += "/" + url.PathEscape(name)
	}
	req, err := s.client.newJSONRequest(ctx, method, path, input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	out := new(Rapid7Source)
	response, err := s.client.Do(req, out)
	if err == nil && strings.TrimSpace(out.Name) == "" {
		out.Name, out.BaseURL, out.CredentialID = input.Name, input.BaseURL, input.CredentialID
	}
	return out, response, err
}

func (s *IntegrationsService) GetServiceNow(ctx context.Context) (*ServiceNowIntegrationSettings, bool, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/integrations/servicenow", nil)
	if err != nil {
		return nil, false, nil, err
	}
	req = markOperation(req, "Integrations.GetServiceNow")
	out := new(ServiceNowIntegrationSettings)
	response, err := s.client.Do(req, out)
	if isStatus(err, http.StatusNotFound) {
		return nil, false, response, nil
	}
	return out, err == nil, response, err
}
func (s *IntegrationsService) DeleteServiceNow(ctx context.Context) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/integrations/servicenow", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Integrations.DeleteServiceNow")
	return s.client.Do(req, nil)
}
func (s *IntegrationsService) PatchServiceNow(ctx context.Context, input ServiceNowIntegrationRequest) (*Response, error) {
	return s.writeServiceNow(ctx, http.MethodPatch, input, "Integrations.PatchServiceNow")
}
func (s *IntegrationsService) writeServiceNow(ctx context.Context, method string, input ServiceNowIntegrationRequest, operation string) (*Response, error) {
	req, err := s.client.newJSONRequest(ctx, method, "/api/integrations/servicenow", input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	return s.client.Do(req, nil)
}

func (s *IntegrationsService) ServiceNowCMDBSchema(ctx context.Context, input ServiceNowCMDBSchemaRequest) ([]ServiceNowCMDBSchema, *Response, error) {
	result := []ServiceNowCMDBSchema{}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb?view=schema", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Integrations.ServiceNowCMDBSchema")
	response, err := s.client.doRequired(req, &result)
	return result, response, err
}
func (s *IntegrationsService) SaveServiceNowCMDB(ctx context.Context, input ServiceNowCMDBConfiguration) (*Response, error) {
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb/configuration", input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Integrations.SaveServiceNowCMDB")
	return s.client.Do(req, nil)
}
func (s *IntegrationsService) EnableServiceNowCMDB(ctx context.Context) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb?action=enable", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Integrations.EnableServiceNowCMDB")
	return s.client.Do(req, nil)
}

func integrationNetworkPath(client *Client, networkID, suffix string) (string, error) {
	if client == nil {
		return "", errors.New("forward: client is nil")
	}
	networkID, err := client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + suffix, nil
}

// deleteTolerant sends a DELETE and counts a plain 404 (the thing is already gone) as success. A 404 that means Forward
// does not serve the route at all (ErrEndpointNotServed) is still an error: nothing was deleted.
func (s *IntegrationsService) deleteTolerant(ctx context.Context, path, operation string) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	response, err := s.client.Do(req, nil)
	if isGone(err) {
		return response, nil
	}
	return response, err
}

// InfobloxInstanceUpdate changes part of an Infoblox instance. Forward's InfobloxInstanceUpdate carries only these
// fields: the address of an instance cannot be edited. Password is write-only.
type InfobloxInstanceUpdate struct {
	Name     *string `json:"name,omitempty"`
	Username *string `json:"username,omitempty"`
	Password *string `json:"password,omitempty"`
}

// UpdateInfoblox patches one Infoblox instance by its numeric id.
// PATCH /api/integrations/infoblox/instances/{instanceId} (InfobloxController.updateInstance, MANAGE_INFOBLOX_INTEGRATION; 204).
func (s *IntegrationsService) UpdateInfoblox(ctx context.Context, instanceID string, patch InfobloxInstanceUpdate) (*Response, error) {
	path, err := infobloxInstancePath(instanceID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Integrations.UpdateInfoblox")
	return s.client.Do(req, nil)
}

// DeleteInfoblox removes one Infoblox instance by its numeric id; an instance that is already gone counts as success.
// DELETE /api/integrations/infoblox/instances/{instanceId} (InfobloxController.deleteInstance, MANAGE_INFOBLOX_INTEGRATION; 204).
func (s *IntegrationsService) DeleteInfoblox(ctx context.Context, instanceID string) (*Response, error) {
	path, err := infobloxInstancePath(instanceID)
	if err != nil {
		return nil, err
	}
	return s.deleteTolerant(ctx, path, "Integrations.DeleteInfoblox")
}

func infobloxInstancePath(instanceID string) (string, error) {
	instanceID = strings.TrimSpace(instanceID)
	if n, err := strconv.Atoi(instanceID); err != nil || n < 0 {
		return "", errors.New("forward: Infoblox instance id must be the numeric instance id")
	}
	return "/api/integrations/infoblox/instances/" + instanceID, nil
}

// DeleteRapid7 removes a Rapid7 source by name; a source that is already gone counts as success.
// DELETE /api/networks/{networkId}/rapid7-sources/{sourceName} (EndHostScannerController.deleteRapid7Source,
// DELETE_COLLECTION_SOURCES; 204).
func (s *IntegrationsService) DeleteRapid7(ctx context.Context, networkID, name string) (*Response, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("forward: Rapid7 source name is required")
	}
	path, err := integrationNetworkPath(s.client, networkID, "/rapid7-sources/"+url.PathEscape(name))
	if err != nil {
		return nil, err
	}
	return s.deleteTolerant(ctx, path, "Integrations.DeleteRapid7")
}
