package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ServiceNow CMDB integration (ServiceNowCmdbController, master fe0daf63c899). Preview: not in the published spec. Every route needs the
// org permission MANAGE_SERVICENOW_CMDB; the ones that reach ServiceNow also need the deployment's outbound connections. Together with
// Integrations.ServiceNowCMDBSchema, SaveServiceNowCMDB (which takes a ServiceNowCMDBConfiguration, the shape Forward also returns) and
// EnableServiceNowCMDB they cover the whole controller.

// ServiceNowCMDBFieldContext says where in a CI a push error happened (Forward's ServiceNowCmdbError.FieldContext).
type ServiceNowCMDBFieldContext struct {
	MatchingAttributesTried []string `json:"matchingAttributesTried,omitempty"`
	CITableName             string   `json:"ciTableName,omitempty"`
	FieldName               string   `json:"fieldName,omitempty"`
	ExpectedType            string   `json:"expectedType,omitempty"`
	ActualValue             string   `json:"actualValue,omitempty"`
	ReferenceTargetTable    string   `json:"referenceTargetTable,omitempty"`
	// NQESource is the NQE query and column that produced the value: queryId, queryName and columnName.
	NQESource map[string]any `json:"nqeSource,omitempty"`
}

// ServiceNowCMDBError is a structured CMDB error (Forward's ServiceNowCmdbError; Count is always present and at least 1). ErrorType is Forward's ServiceNowErrorType, for example
// AUTH_INVALID_CREDENTIALS, CONNECTION_SSL_ERROR, PERMISSION_ACL_RESTRICTION, SCHEMA_FIELD_NOT_FOUND or UNKNOWN_ERROR; it is a free string
// here because ServiceNow instances add cases.
type ServiceNowCMDBError struct {
	ErrorType        string                      `json:"errorType"`
	UserMessage      string                      `json:"userMessage"`
	TechnicalDetails string                      `json:"technicalDetails,omitempty"`
	FieldContext     *ServiceNowCMDBFieldContext `json:"fieldContext,omitempty"`
	RemediationSteps string                      `json:"remediationSteps,omitempty"`
	Count            int                         `json:"count,omitempty"`
}

// ServiceNowCMDBTestResult is the answer of a connection test: empty when the connection works.
type ServiceNowCMDBTestResult struct {
	ErrorMessage string               `json:"errorMessage,omitempty"`
	Error        *ServiceNowCMDBError `json:"error,omitempty"`
}

// OK reports whether the test succeeded.
func (r ServiceNowCMDBTestResult) OK() bool { return r.ErrorMessage == "" && r.Error == nil }

// CMDB integration statuses (Forward's CmdbIntegrationStatus).
const (
	ServiceNowCMDBStatusSuccessful        = "SUCCESSFUL"
	ServiceNowCMDBStatusConnectionFailed  = "CONNECTION_FAILED"
	ServiceNowCMDBStatusIncorrectMappings = "INCORRECT_MAPPINGS"
)

// ServiceNowCMDBStatus is the health of the saved configuration (Forward's CmdbConfigurationStatus).
type ServiceNowCMDBStatus struct {
	Status                     string   `json:"status"`
	MissingClassNames          []string `json:"missingClassNames,omitempty"`
	IncorrectMappingClassNames []string `json:"incorrectMappingClassNames,omitempty"`
}

// ServiceNowCMDBSyncRun is one network's push (Forward's CmdbSynchronizationInfo). SyncInstant is milliseconds since the epoch. Error is set
// when the push failed; EntitiesInserted and EntitiesUpdated are set when it did not.
type ServiceNowCMDBSyncRun struct {
	SyncInstant      int64                 `json:"syncInstant"`
	NetworkName      string                `json:"networkName"`
	Error            string                `json:"error,omitempty"`
	EntitiesInserted *int64                `json:"entitiesInserted,omitempty"`
	EntitiesUpdated  *int64                `json:"entitiesUpdated,omitempty"`
	Errors           []ServiceNowCMDBError `json:"errors,omitempty"`
}

// ServiceNowCMDBPayloadTable is the rows Forward would push to one ServiceNow table (Forward's CmdbTablePayload). A row is the mapped
// columns of one entity, keyed by the CMDB field's element name.
type ServiceNowCMDBPayloadTable struct {
	TableName string           `json:"tableName"`
	Rows      []map[string]any `json:"rows"`
}

// ServiceNowCMDBPayloadPreview is what a push for a network would send (Forward's CmdbPayloadPreview). Exactly one of Error and Tables is
// set.
type ServiceNowCMDBPayloadPreview struct {
	Error  string                       `json:"error,omitempty"`
	Tables []ServiceNowCMDBPayloadTable `json:"tables,omitempty"`
}

func (s *IntegrationsService) postCMDBConnection(ctx context.Context, rawQuery string, connection ServiceNowConnectionDetails, operation string, out any) (*Response, error) {
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb"+rawQuery, connection)
	if err != nil {
		return nil, err
	}
	return s.client.doRequired(markOperation(req, operation), out)
}

// ServiceNowCMDBDiscoverySources lists the discovery sources the instance offers for the CMDB source name (POST /integrations/servicenow-cmdb
// ?view=discovery-sources). It calls out to ServiceNow with the given connection.
func (s *IntegrationsService) ServiceNowCMDBDiscoverySources(ctx context.Context, connection ServiceNowConnectionDetails) ([]string, *Response, error) {
	out := []string{}
	resp, err := s.postCMDBConnection(ctx, "?view=discovery-sources", connection, "Integrations.ServiceNowCMDBDiscoverySources", &out)
	return out, resp, err
}

// ServiceNowCMDBConfigurationItems lists the configuration item classes the instance has (POST /integrations/servicenow-cmdb
// ?view=configuration-items). It calls out to ServiceNow with the given connection.
func (s *IntegrationsService) ServiceNowCMDBConfigurationItems(ctx context.Context, connection ServiceNowConnectionDetails) ([]string, *Response, error) {
	out := []string{}
	resp, err := s.postCMDBConnection(ctx, "?view=configuration-items", connection, "Integrations.ServiceNowCMDBConfigurationItems", &out)
	return out, resp, err
}

// TestServiceNowCMDB checks that Forward can reach ServiceNow with the given connection (POST /integrations/servicenow-cmdb/test). A failed
// connection is a 200 with the failure in the result, not an HTTP error. Forward may take a while to give up on an unreachable instance.
func (s *IntegrationsService) TestServiceNowCMDB(ctx context.Context, connection ServiceNowConnectionDetails) (*ServiceNowCMDBTestResult, *Response, error) {
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb/test", connection)
	if err != nil {
		return nil, nil, err
	}
	out := new(ServiceNowCMDBTestResult)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.TestServiceNowCMDB"), out)
	return out, resp, err
}

// GetServiceNowCMDBConfiguration reads the saved configuration (GET /integrations/servicenow-cmdb/configuration). The second result is
// false when none is saved (404). The connection's secrets come back as empty strings.
func (s *IntegrationsService) GetServiceNowCMDBConfiguration(ctx context.Context) (*ServiceNowCMDBConfiguration, bool, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/integrations/servicenow-cmdb/configuration", nil)
	if err != nil {
		return nil, false, nil, err
	}
	out := new(ServiceNowCMDBConfiguration)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.GetServiceNowCMDBConfiguration"), out)
	if err != nil {
		if isGone(err) {
			return nil, false, resp, nil
		}
		return nil, false, resp, err
	}
	return out, true, resp, nil
}

// DisableServiceNowCMDB switches the CMDB sync off without deleting the configuration (POST /integrations/servicenow-cmdb?action=disable,
// 204).
func (s *IntegrationsService) DisableServiceNowCMDB(ctx context.Context) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb?action=disable", nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Integrations.DisableServiceNowCMDB"), nil)
}

// DeleteServiceNowCMDBConfiguration deletes the saved configuration, its connection and its mappings (DELETE
// /integrations/servicenow-cmdb/configuration).
func (s *IntegrationsService) DeleteServiceNowCMDBConfiguration(ctx context.Context) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/integrations/servicenow-cmdb/configuration", nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Integrations.DeleteServiceNowCMDBConfiguration"), nil)
}

// PushServiceNowCMDB starts a push of the configured networks to ServiceNow (POST /integrations/servicenow-cmdb?action=push, 204). It
// writes configuration items into the customer's CMDB. The push runs after the answer: read the outcome with ServiceNowCMDBSyncHistory.
func (s *IntegrationsService) PushServiceNowCMDB(ctx context.Context) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/integrations/servicenow-cmdb?action=push", nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Integrations.PushServiceNowCMDB"), nil)
}

// ServiceNowCMDBSyncHistory lists the recent pushes (GET /integrations/servicenow-cmdb/synchronization-history). It is empty before the
// first push (Forward answers {"infos":[]}, not 404).
func (s *IntegrationsService) ServiceNowCMDBSyncHistory(ctx context.Context) ([]ServiceNowCMDBSyncRun, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/integrations/servicenow-cmdb/synchronization-history", nil)
	if err != nil {
		return nil, nil, err
	}
	var out struct {
		Infos []ServiceNowCMDBSyncRun `json:"infos"`
	}
	resp, err := s.client.doRequired(markOperation(req, "Integrations.ServiceNowCMDBSyncHistory"), &out)
	return out.Infos, resp, err
}

// ServiceNowCMDBStatusReport checks the saved configuration against the instance (GET /integrations/servicenow-cmdb/status). It calls out
// to ServiceNow, and answers 404 while no configuration is saved.
func (s *IntegrationsService) ServiceNowCMDBStatusReport(ctx context.Context) (*ServiceNowCMDBStatus, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/integrations/servicenow-cmdb/status", nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ServiceNowCMDBStatus)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.ServiceNowCMDBStatusReport"), out)
	return out, resp, err
}

// ServiceNowCMDBPayload previews what a push for one network would send (GET /integrations/servicenow-cmdb?view=payload&networkId=). A
// problem building it comes back in the result's Error, not as an HTTP error.
func (s *IntegrationsService) ServiceNowCMDBPayload(ctx context.Context, networkID string) (*ServiceNowCMDBPayloadPreview, *Response, error) {
	if networkID = strings.TrimSpace(networkID); networkID == "" {
		return nil, nil, errors.New("forward: network ID is required")
	}
	query := url.Values{"view": {"payload"}, "networkId": {networkID}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/integrations/servicenow-cmdb?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ServiceNowCMDBPayloadPreview)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.ServiceNowCMDBPayload"), out)
	return out, resp, err
}

// DownloadServiceNowCMDBPayload writes the payload preview of every configured network as a ZIP to w (GET /integrations/servicenow-cmdb
// ?view=payload&disposition=attachment). A failure mid-stream leaves partial bytes in w with the error.
func (s *IntegrationsService) DownloadServiceNowCMDBPayload(ctx context.Context, w io.Writer) (*Response, error) {
	return s.downloadCMDB(ctx, "/api/integrations/servicenow-cmdb?view=payload&disposition=attachment", "application/zip", "Integrations.DownloadServiceNowCMDBPayload", w)
}

// DownloadServiceNowCMDBSyncReport writes the last sync's report as an .xlsx workbook to w (GET
// /integrations/servicenow-cmdb/synchronization-report).
func (s *IntegrationsService) DownloadServiceNowCMDBSyncReport(ctx context.Context, w io.Writer) (*Response, error) {
	return s.downloadCMDB(ctx, "/api/integrations/servicenow-cmdb/synchronization-report", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "Integrations.DownloadServiceNowCMDBSyncReport", w)
}

func (s *IntegrationsService) downloadCMDB(ctx context.Context, path, accept, operation string, w io.Writer) (*Response, error) {
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
