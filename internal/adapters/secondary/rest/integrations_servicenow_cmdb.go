package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ServiceNow CMDB integration (ServiceNowCmdbController, master fe0daf63c899). Preview: not in the published spec. Every route needs the
// org permission MANAGE_SERVICENOW_CMDB; the ones that reach ServiceNow also need the deployment's outbound connections. Together with
// Integrations.ServiceNowCMDBSchema, SaveServiceNowCMDB (which takes a ServiceNowCMDBConfiguration, the shape Forward also returns) and
// EnableServiceNowCMDB they cover the whole controller.

func (s *Integrations) postCMDBConnection(ctx context.Context, rawQuery string, connection ports.ServiceNowConnectionDetails, operation string, out any) (*ports.Response, error) {
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb"+rawQuery, connection)
	if err != nil {
		return nil, err
	}
	return s.client.doRequired(markOperation(req, operation), out)
}

// ServiceNowCMDBDiscoverySources lists the discovery sources the instance offers for the CMDB source name (POST /integrations/servicenow-cmdb
// ?view=discovery-sources). It calls out to ServiceNow with the given connection.
func (s *Integrations) ServiceNowCMDBDiscoverySources(ctx context.Context, connection ports.ServiceNowConnectionDetails) ([]string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := []string{}
	resp, err := s.postCMDBConnection(ctx, "?view=discovery-sources", connection, "Integrations.ServiceNowCMDBDiscoverySources", &out)
	return out, resp, err
}

// ServiceNowCMDBConfigurationItems lists the configuration item classes the instance has (POST /integrations/servicenow-cmdb
// ?view=configuration-items). It calls out to ServiceNow with the given connection.
func (s *Integrations) ServiceNowCMDBConfigurationItems(ctx context.Context, connection ports.ServiceNowConnectionDetails) ([]string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := []string{}
	resp, err := s.postCMDBConnection(ctx, "?view=configuration-items", connection, "Integrations.ServiceNowCMDBConfigurationItems", &out)
	return out, resp, err
}

// TestServiceNowCMDB checks that Forward can reach ServiceNow with the given connection (POST /integrations/servicenow-cmdb/test). A failed
// connection is a 200 with the failure in the result, not an HTTP error. Forward may take a while to give up on an unreachable instance.
func (s *Integrations) TestServiceNowCMDB(ctx context.Context, connection ports.ServiceNowConnectionDetails) (*ports.ServiceNowCMDBTestResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb/test", connection)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.ServiceNowCMDBTestResult)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.TestServiceNowCMDB"), out)
	return out, resp, err
}

// GetServiceNowCMDBConfiguration reads the saved configuration (GET /integrations/servicenow-cmdb/configuration). The second result is
// false when none is saved (404). The connection's secrets come back as empty strings.
func (s *Integrations) GetServiceNowCMDBConfiguration(ctx context.Context) (*ports.ServiceNowCMDBConfiguration, bool, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, false, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/integrations/servicenow-cmdb/configuration", nil)
	if err != nil {
		return nil, false, nil, err
	}
	out := new(ports.ServiceNowCMDBConfiguration)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.GetServiceNowCMDBConfiguration"), out)
	if err != nil {
		if ports.IsGone(err) {
			return nil, false, resp, nil
		}
		return nil, false, resp, err
	}
	return out, true, resp, nil
}

// DisableServiceNowCMDB switches the CMDB sync off without deleting the configuration (POST /integrations/servicenow-cmdb?action=disable,
// 204).
func (s *Integrations) DisableServiceNowCMDB(ctx context.Context) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb?action=disable", nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Integrations.DisableServiceNowCMDB"), nil)
}

// DeleteServiceNowCMDBConfiguration deletes the saved configuration, its connection and its mappings (DELETE
// /integrations/servicenow-cmdb/configuration).
func (s *Integrations) DeleteServiceNowCMDBConfiguration(ctx context.Context) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/integrations/servicenow-cmdb/configuration", nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Integrations.DeleteServiceNowCMDBConfiguration"), nil)
}

// PushServiceNowCMDB starts a push of the configured networks to ServiceNow (POST /integrations/servicenow-cmdb?action=push, 204). It
// writes configuration items into the customer's CMDB. The push runs after the answer: read the outcome with ServiceNowCMDBSyncHistory.
func (s *Integrations) PushServiceNowCMDB(ctx context.Context) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb?action=push", nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Integrations.PushServiceNowCMDB"), nil)
}

// ServiceNowCMDBSyncHistory lists the recent pushes (GET /integrations/servicenow-cmdb/synchronization-history). It is empty before the
// first push (Forward answers {"infos":[]}, not 404).
func (s *Integrations) ServiceNowCMDBSyncHistory(ctx context.Context) ([]ports.ServiceNowCMDBSyncRun, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/integrations/servicenow-cmdb/synchronization-history", nil)
	if err != nil {
		return nil, nil, err
	}
	var out struct {
		Infos []ports.ServiceNowCMDBSyncRun `json:"infos"`
	}
	resp, err := s.client.doRequired(markOperation(req, "Integrations.ServiceNowCMDBSyncHistory"), &out)
	return out.Infos, resp, err
}

// ServiceNowCMDBStatusReport checks the saved configuration against the instance (GET /integrations/servicenow-cmdb/status). It calls out
// to ServiceNow, and answers 404 while no configuration is saved.
func (s *Integrations) ServiceNowCMDBStatusReport(ctx context.Context) (*ports.ServiceNowCMDBStatus, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/integrations/servicenow-cmdb/status", nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.ServiceNowCMDBStatus)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.ServiceNowCMDBStatusReport"), out)
	return out, resp, err
}

// ServiceNowCMDBPayload previews what a push for one network would send (GET /integrations/servicenow-cmdb?view=payload&networkId=). A
// problem building it comes back in the result's Error, not as an HTTP error.
func (s *Integrations) ServiceNowCMDBPayload(ctx context.Context, networkID string) (*ports.ServiceNowCMDBPayloadPreview, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if networkID = strings.TrimSpace(networkID); networkID == "" {
		return nil, nil, errors.New("forward: network ID is required")
	}
	query := url.Values{"view": {"payload"}, "networkId": {networkID}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/integrations/servicenow-cmdb?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.ServiceNowCMDBPayloadPreview)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.ServiceNowCMDBPayload"), out)
	return out, resp, err
}

// DownloadServiceNowCMDBPayload writes the payload preview of every configured network as a ZIP to w (GET /integrations/servicenow-cmdb
// ?view=payload&disposition=attachment). A failure mid-stream leaves partial bytes in w with the error.
func (s *Integrations) DownloadServiceNowCMDBPayload(ctx context.Context, w io.Writer) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.downloadCMDB(ctx, "/api/integrations/servicenow-cmdb?view=payload&disposition=attachment", "application/zip", "Integrations.DownloadServiceNowCMDBPayload", w)
}

// DownloadServiceNowCMDBSyncReport writes the last sync's report as an .xlsx workbook to w (GET
// /integrations/servicenow-cmdb/synchronization-report).
func (s *Integrations) DownloadServiceNowCMDBSyncReport(ctx context.Context, w io.Writer) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.downloadCMDB(ctx, "/api/integrations/servicenow-cmdb/synchronization-report", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "Integrations.DownloadServiceNowCMDBSyncReport", w)
}

func (s *Integrations) downloadCMDB(ctx context.Context, path, accept, operation string, w io.Writer) (*ports.Response, error) {
	if w == nil {
		return nil, errors.New("forward: a writer is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept+", application/json")
	return s.client.Do(markOperation(req, operation), w)
}
