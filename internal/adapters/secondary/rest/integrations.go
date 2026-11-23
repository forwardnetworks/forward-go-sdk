package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Integrations service

// ListInfoblox returns the organization's Infoblox instances (GET /api/integrations/infoblox, whose body is {"enabled":..,"instances":[..]};
// InfobloxController.getInfobloxIntegration, MANAGE_INFOBLOX_INTEGRATION). Before v0.8.38 this read GET /api/integrations/infoblox/instances
// without a query, a route Forward maps only with ?view=status (it answers the instances' connection status, not the instances): every build
// answered it 400 `Parameter conditions "view=status" not met`, seen live by forward-skills on 26.10.6. See InfobloxStatus for that route.
func (s *Integrations) ListInfoblox(ctx context.Context) ([]ports.InfobloxInstance, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.listInfoblox(ctx, "/api/integrations/infoblox", "Integrations.ListInfoblox")
}

// InfobloxStatus tests every configured instance and returns each one's status (GET /api/integrations/infoblox/instances?view=status;
// InfobloxController.getStatus, MANAGE_INFOBLOX_INTEGRATION). It contacts each Infoblox appliance, so it can be slow. Preview: shape from
// Forward's source (InfobloxInstancesStatus), not seen live.
func (s *Integrations) InfobloxStatus(ctx context.Context) ([]ports.InfobloxInstanceStatus, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.InfobloxInstanceStatus]{Keys: []string{"instances"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/integrations/infoblox/instances?view=status", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Integrations.InfobloxStatus")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// ListInfobloxLegacy is ListInfoblox on the same route; kept for callers of the older name.
func (s *Integrations) ListInfobloxLegacy(ctx context.Context) ([]ports.InfobloxInstance, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.listInfoblox(ctx, "/api/integrations/infoblox", "Integrations.ListInfobloxLegacy")
}

func (s *Integrations) listInfoblox(ctx context.Context, path, operation string) ([]ports.InfobloxInstance, *ports.Response, error) {
	result := listResponse[ports.InfobloxInstance]{Keys: []string{"instances", "items", "data"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *Integrations) CreateInfoblox(ctx context.Context, input ports.InfobloxInstanceRequest) (*ports.InfobloxInstance, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/integrations/infoblox/instances", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Integrations.CreateInfoblox")
	out := new(ports.InfobloxInstance)
	response, err := s.client.Do(req, out)
	if err == nil && strings.TrimSpace(out.Name) == "" {
		out.Name, out.IPAddress, out.Username = input.Name, input.IPAddress, input.Username
	}
	return out, response, err
}

func (s *Integrations) ListRapid7(ctx context.Context, networkID string) ([]ports.Rapid7Source, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := integrationNetworkPath(s.client, networkID, "/end-host-scanners")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.Rapid7Source]{Keys: []string{"sources", "scanners", "items", "data"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Integrations.ListRapid7")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *Integrations) CreateRapid7(ctx context.Context, networkID string, input ports.Rapid7SourceRequest) (*ports.Rapid7Source, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.writeRapid7(ctx, http.MethodPost, networkID, "", input, "Integrations.CreateRapid7")
}

func (s *Integrations) UpdateRapid7(ctx context.Context, networkID, name string, input ports.Rapid7SourceRequest) (*ports.Rapid7Source, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.writeRapid7(ctx, http.MethodPatch, networkID, name, input, "Integrations.UpdateRapid7")
}

func (s *Integrations) writeRapid7(ctx context.Context, method, networkID, name string, input ports.Rapid7SourceRequest, operation string) (*ports.Rapid7Source, *ports.Response, error) {
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
	out := new(ports.Rapid7Source)
	response, err := s.client.Do(req, out)
	if err == nil && strings.TrimSpace(out.Name) == "" {
		out.Name, out.BaseURL, out.CredentialID = input.Name, input.BaseURL, input.CredentialID
	}
	return out, response, err
}

func (s *Integrations) GetServiceNow(ctx context.Context) (*ports.ServiceNowIntegrationSettings, bool, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, false, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/integrations/servicenow", nil)
	if err != nil {
		return nil, false, nil, err
	}
	req = markOperation(req, "Integrations.GetServiceNow")
	out := new(ports.ServiceNowIntegrationSettings)
	response, err := s.client.Do(req, out)
	if ports.IsStatus(err, http.StatusNotFound) {
		return nil, false, response, nil
	}
	return out, err == nil, response, err
}

func (s *Integrations) DeleteServiceNow(ctx context.Context) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/integrations/servicenow", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Integrations.DeleteServiceNow")
	return s.client.Do(req, nil)
}

func (s *Integrations) PatchServiceNow(ctx context.Context, input ports.ServiceNowIntegrationRequest) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.writeServiceNow(ctx, http.MethodPatch, input, "Integrations.PatchServiceNow")
}

// UpdateServiceNow changes some fields of the ServiceNow connection and leaves the rest (PATCH /integrations/servicenow, the same route
// as PatchServiceNow). It sends only the fields that are set, so it cannot switch a flag off by leaving it out. The first call for an org
// has to carry InstanceURL and either a username and password or a token, because Forward builds the connection from it. Token and
// username/password together are refused here, as Forward refuses them.
func (s *Integrations) UpdateServiceNow(ctx context.Context, update ports.ServiceNowSettingsUpdate) (*ports.ServiceNowIntegrationSettings, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if update.Token != nil && (update.Username != nil || update.Password != nil) {
		return nil, nil, errors.New("forward: a ServiceNow connection takes a username and password or a token, not both")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/integrations/servicenow", update)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.ServiceNowIntegrationSettings)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.UpdateServiceNow"), out)
	return out, resp, err
}

func (s *Integrations) writeServiceNow(ctx context.Context, method string, input ports.ServiceNowIntegrationRequest, operation string) (*ports.Response, error) {
	req, err := s.client.newJSONRequest(ctx, method, "/api/integrations/servicenow", input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	return s.client.Do(req, nil)
}

func (s *Integrations) ServiceNowCMDBSchema(ctx context.Context, input ports.ServiceNowCMDBSchemaRequest) ([]ports.ServiceNowCMDBSchema, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	result := []ports.ServiceNowCMDBSchema{}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb?view=schema", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Integrations.ServiceNowCMDBSchema")
	response, err := s.client.doRequired(req, &result)
	return result, response, err
}

func (s *Integrations) SaveServiceNowCMDB(ctx context.Context, input ports.ServiceNowCMDBConfiguration) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb/configuration", input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Integrations.SaveServiceNowCMDB")
	return s.client.Do(req, nil)
}

func (s *Integrations) EnableServiceNowCMDB(ctx context.Context) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *Integrations) deleteTolerant(ctx context.Context, path, operation string) (*ports.Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	response, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
		return response, nil
	}
	return response, err
}

// UpdateInfoblox patches one Infoblox instance by its numeric id.
// PATCH /api/integrations/infoblox/instances/{instanceId} (InfobloxController.updateInstance, MANAGE_INFOBLOX_INTEGRATION; 204).
func (s *Integrations) UpdateInfoblox(ctx context.Context, instanceID string, patch ports.InfobloxInstanceUpdate) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *Integrations) DeleteInfoblox(ctx context.Context, instanceID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *Integrations) DeleteRapid7(ctx context.Context, networkID, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
