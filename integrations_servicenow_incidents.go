package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// ServiceNow incidents for failing checks (ServiceNowController and CheckController, master fe0daf63c899). Preview: not in the published
// spec. Everything here needs the deployment's outbound connections and the org's ServiceNow settings (Integrations.GetServiceNow).
// Listing, the update history and refreshing need VIEW_CHECK_INTEGRATIONS and changing needs OPERATE_CHECK_INTEGRATIONS (get-incident asks for
// no org permission); the check routes also need VIEW_CHECKS on the network.

// ServiceNow incident levels, impact and urgency, on the wire: ServiceNow's numeric value as a JSON string (Forward's
// ServiceNowIncidentLevel). Forward maps any other number to LOW, so a customised instance's own values read back as "3".
const (
	ServiceNowIncidentLevelHigh   = "1"
	ServiceNowIncidentLevelMedium = "2"
	ServiceNowIncidentLevelLow    = "3"
)

// ServiceNowIncidentInput is an incident to raise for a check (Forward's NewServiceNowIncident). Summary, Impact and Urgency are required.
// This SDK accepts only the levels "1", "2" and "3": Forward would take any other number and quietly store LOW.
type ServiceNowIncidentInput struct {
	Summary string `json:"summary"`
	Details string `json:"details,omitempty"`
	Impact  string `json:"impact"`
	Urgency string `json:"urgency"`
}

// ServiceNowIncident is an incident read from ServiceNow (Forward's ServiceNowIncident). Impact and Urgency are the numeric strings above;
// State is ServiceNow's label (New, In Progress, On Hold, Resolved, Closed, Canceled).
type ServiceNowIncident struct {
	Number  string `json:"number"`
	SysID   string `json:"sysId"`
	Summary string `json:"summary,omitempty"`
	Details string `json:"details,omitempty"`
	Impact  string `json:"impact"`
	Urgency string `json:"urgency"`
	State   string `json:"state"`
	URL     string `json:"url"`
}

// ServiceNowIncidentUpdate is one work-note update Forward made to an incident: the snapshot it described (milliseconds since the epoch) and
// how many failures that snapshot had.
type ServiceNowIncidentUpdate struct {
	SnapshotCreationTime int64 `json:"snapshotCreationTime"`
	TotalFailures        int64 `json:"totalFailures"`
}

// ServiceNowIncidentLink is an incident Forward holds for a check (Forward's ServiceNowIncidentBasicInfo). Forward serializes only these four
// fields (the class turns getter detection off and annotates just them), so the incident's sys id, whether it was auto-created and the
// snapshot window it applies to are not available from here; CheckIncidentUpdateHistory gives the update history, and
// GetServiceNowIncident the sys id. StateLastUpdated is milliseconds since the epoch.
type ServiceNowIncidentLink struct {
	CheckID          Identifier `json:"checkId"`
	Number           string     `json:"number"`
	State            string     `json:"state"`
	StateLastUpdated int64      `json:"stateLastUpdated"`
}

func incidentNumberPath(number string) (string, error) {
	if number = strings.TrimSpace(number); number == "" {
		return "", errors.New("forward: an incident number is required")
	}
	return "/api/integrations/servicenow/incidents/" + url.PathEscape(number), nil
}

func validateIncidentInput(input ServiceNowIncidentInput) error {
	if strings.TrimSpace(input.Summary) == "" {
		return errors.New("forward: an incident needs a summary")
	}
	for _, level := range []string{input.Impact, input.Urgency} {
		switch level {
		case ServiceNowIncidentLevelHigh, ServiceNowIncidentLevelMedium, ServiceNowIncidentLevelLow:
		default:
			return errors.New(`forward: an incident's impact and urgency are "1", "2" or "3"`)
		}
	}
	return nil
}

// GetServiceNowIncident reads an incident from the org's ServiceNow instance by its number (GET /integrations/servicenow/incidents/{number}).
func (s *IntegrationsService) GetServiceNowIncident(ctx context.Context, number string) (*ServiceNowIncident, *Response, error) {
	path, err := incidentNumberPath(number)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ServiceNowIncident)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.GetServiceNowIncident"), out)
	return out, resp, err
}

// CloseServiceNowIncident closes one incident in ServiceNow (POST /integrations/servicenow/incidents/{number}?action=close). It changes the
// customer's ServiceNow record.
func (s *IntegrationsService) CloseServiceNowIncident(ctx context.Context, number string) (*Response, error) {
	path, err := incidentNumberPath(number)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"?action=close", nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Integrations.CloseServiceNowIncident"), nil)
}

// CloseServiceNowIncidents closes several incidents in ServiceNow (POST /integrations/servicenow/incidents?action=close, 204). It changes
// the customer's ServiceNow records.
func (s *IntegrationsService) CloseServiceNowIncidents(ctx context.Context, numbers []string) (*Response, error) {
	cleaned := make([]string, 0, len(numbers))
	for _, number := range numbers {
		if number = strings.TrimSpace(number); number != "" {
			cleaned = append(cleaned, number)
		}
	}
	if len(cleaned) == 0 {
		return nil, errors.New("forward: at least one incident number is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/integrations/servicenow/incidents?action=close", cleaned)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Integrations.CloseServiceNowIncidents"), nil)
}

// Check types the incident view filters by, spelled as Forward's source parses the `type` query parameter (CheckType.fromJson on master;
// the legacy spellings also work). Master's source rejects the Java enum names ("ISOLATION"), but a live 26.10.5 build was seen to answer
// 200 to one, which the source does not explain: send these spellings, which both accept, and do not rely on either behaviour for the names.
// An unknown type is a 400 "Invalid check type" and no type at all is a 400 "At least one 'type' is required".
const (
	CheckTypeIsolation    = "Isolation"
	CheckTypeReachability = "Reachability"
	CheckTypeExists       = "Existential"
	CheckTypeQueryString  = "QueryStringBased"
	CheckTypePredefined   = "Predefined"
	CheckTypeNetworkQuery = "NQE"
)

func checkIncidentsPath(snapshotID string, rest ...string) (string, error) {
	if len(rest) == 0 {
		return checksPath(snapshotID)
	}
	return checkPath(snapshotID, rest[0])
}

// ListCheckIncidents lists the ServiceNow incidents linked to the snapshot's active checks of the given types (GET
// /snapshots/{id}/checks?view=serviceNowIncidents&type=...). At least one type is required; Forward answers 400 for a name it does not know.
func (s *IntegrationsService) ListCheckIncidents(ctx context.Context, snapshotID string, checkTypes []string) ([]ServiceNowIncidentLink, *Response, error) {
	path, err := checkIncidentsPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"view": {"serviceNowIncidents"}}
	for _, checkType := range checkTypes {
		if checkType = strings.TrimSpace(checkType); checkType != "" {
			query.Add("type", checkType)
		}
	}
	if len(query["type"]) == 0 {
		return nil, nil, errors.New("forward: at least one check type is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	out := []ServiceNowIncidentLink{}
	resp, err := s.client.doRequired(markOperation(req, "Integrations.ListCheckIncidents"), &out)
	return out, resp, err
}

// AddCheckIncident raises a ServiceNow incident for one failing check and links it (POST /snapshots/{id}/checks/{checkId}
// ?action=addServiceNowIncident). Forward refuses a check that is disabled or not failing in this snapshot and every later one. It creates
// a record in the customer's ServiceNow.
func (s *IntegrationsService) AddCheckIncident(ctx context.Context, snapshotID, checkID string, input ServiceNowIncidentInput) (*ServiceNowIncidentLink, *Response, error) {
	if err := validateIncidentInput(input); err != nil {
		return nil, nil, err
	}
	path, err := checkIncidentsPath(snapshotID, checkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?"+"action="+"addServiceNowIncident", input)
	if err != nil {
		return nil, nil, err
	}
	out := new(ServiceNowIncidentLink)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.AddCheckIncident"), out)
	return out, resp, err
}

// AddCheckIncidents raises incidents for several checks in one call, keyed by check ID (POST /snapshots/{id}/checks
// ?action=addServiceNowIncidents). The same conditions as AddCheckIncident apply to every check.
func (s *IntegrationsService) AddCheckIncidents(ctx context.Context, snapshotID string, inputs map[string]ServiceNowIncidentInput) ([]ServiceNowIncidentLink, *Response, error) {
	if len(inputs) == 0 {
		return nil, nil, errors.New("forward: at least one check incident is required")
	}
	for checkID, input := range inputs {
		if strings.TrimSpace(checkID) == "" {
			return nil, nil, errors.New("forward: check ID is required")
		}
		if err := validateIncidentInput(input); err != nil {
			return nil, nil, err
		}
	}
	path, err := checkIncidentsPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?"+"action="+"addServiceNowIncidents", inputs)
	if err != nil {
		return nil, nil, err
	}
	out := []ServiceNowIncidentLink{}
	resp, err := s.client.doRequired(markOperation(req, "Integrations.AddCheckIncidents"), &out)
	return out, resp, err
}

// LinkCheckIncident links an incident that already exists in ServiceNow to a check (POST /snapshots/{id}/checks/{checkId}
// ?action=linkServiceNowIncident&incident=NUMBER).
func (s *IntegrationsService) LinkCheckIncident(ctx context.Context, snapshotID, checkID, incidentNumber string) (*ServiceNowIncidentLink, *Response, error) {
	return s.checkIncidentAction(ctx, snapshotID, checkID, incidentNumber, "linkServiceNowIncident", "Integrations.LinkCheckIncident", true)
}

// UnlinkCheckIncident removes the link between a check and an incident (POST /snapshots/{id}/checks/{checkId}
// ?action=unlinkServiceNowIncident&incident=NUMBER). It leaves the incident in ServiceNow alone.
func (s *IntegrationsService) UnlinkCheckIncident(ctx context.Context, snapshotID, checkID, incidentNumber string) (*Response, error) {
	_, resp, err := s.checkIncidentAction(ctx, snapshotID, checkID, incidentNumber, "unlinkServiceNowIncident", "Integrations.UnlinkCheckIncident", false)
	return resp, err
}

func (s *IntegrationsService) checkIncidentAction(ctx context.Context, snapshotID, checkID, incidentNumber, action, operation string, wantBody bool) (*ServiceNowIncidentLink, *Response, error) {
	if incidentNumber = strings.TrimSpace(incidentNumber); incidentNumber == "" {
		return nil, nil, errors.New("forward: an incident number is required")
	}
	path, err := checkIncidentsPath(snapshotID, checkID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"action": {action}, "incident": {incidentNumber}}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	if !wantBody {
		resp, err := s.client.Do(req, nil)
		return nil, resp, err
	}
	out := new(ServiceNowIncidentLink)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// RefreshCheckIncidents re-reads the state of every incident linked to the snapshot's checks from ServiceNow (POST /snapshots/{id}/checks
// ?action=refreshServiceNowIncidents, 204).
func (s *IntegrationsService) RefreshCheckIncidents(ctx context.Context, snapshotID string) (*Response, error) {
	path, err := checkIncidentsPath(snapshotID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"?"+"action="+"refreshServiceNowIncidents", nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Integrations.RefreshCheckIncidents"), nil)
}

// CheckIncidentUpdateHistory lists the work-note updates Forward made to one incident of a check (GET /snapshots/{id}/checks/{checkId}
// ?view=serviceNowIncidentUpdateHistory&incident=NUMBER). An incident that is not linked to the check is a 400.
func (s *IntegrationsService) CheckIncidentUpdateHistory(ctx context.Context, snapshotID, checkID, incidentNumber string) ([]ServiceNowIncidentUpdate, *Response, error) {
	if incidentNumber = strings.TrimSpace(incidentNumber); incidentNumber == "" {
		return nil, nil, errors.New("forward: an incident number is required")
	}
	path, err := checkIncidentsPath(snapshotID, checkID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"view": {"serviceNowIncidentUpdateHistory"}, "incident": {incidentNumber}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	out := []ServiceNowIncidentUpdate{}
	resp, err := s.client.doRequired(markOperation(req, "Integrations.CheckIncidentUpdateHistory"), &out)
	return out, resp, err
}
