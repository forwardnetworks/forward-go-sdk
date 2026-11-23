package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// ServiceNow incidents for failing checks (ServiceNowController and CheckController, master fe0daf63c899). Preview: not in the published
// spec. Everything here needs the deployment's outbound connections and the org's ServiceNow settings (Integrations.GetServiceNow).
// Listing, the update history and refreshing need VIEW_CHECK_INTEGRATIONS and changing needs OPERATE_CHECK_INTEGRATIONS (get-incident asks for
// no org permission); the check routes also need VIEW_CHECKS on the network.

func incidentNumberPath(number string) (string, error) {
	if number = strings.TrimSpace(number); number == "" {
		return "", errors.New("forward: an incident number is required")
	}
	return "/api/integrations/servicenow/incidents/" + url.PathEscape(number), nil
}

func validateIncidentInput(input ports.ServiceNowIncidentInput) error {
	if strings.TrimSpace(input.Summary) == "" {
		return errors.New("forward: an incident needs a summary")
	}
	for _, level := range []string{input.Impact, input.Urgency} {
		switch level {
		case ports.ServiceNowIncidentLevelHigh, ports.ServiceNowIncidentLevelMedium, ports.ServiceNowIncidentLevelLow:
		default:
			return errors.New(`forward: an incident's impact and urgency are "1", "2" or "3"`)
		}
	}
	return nil
}

// GetServiceNowIncident reads an incident from the org's ServiceNow instance by its number (GET /integrations/servicenow/incidents/{number}).
func (s *Integrations) GetServiceNowIncident(ctx context.Context, number string) (*ports.ServiceNowIncident, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := incidentNumberPath(number)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.ServiceNowIncident)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.GetServiceNowIncident"), out)
	return out, resp, err
}

// CloseServiceNowIncident closes one incident in ServiceNow (POST /integrations/servicenow/incidents/{number}?action=close). It changes the
// customer's ServiceNow record. It does not detach the incident from its check: Forward answers 400 "Cannot close incident X. Incident is
// still active." while the incident still applies to a check (no end instant yet), and UnlinkCheckIncident is what detaches it. An incident
// already Closed in ServiceNow is not touched there; Forward still marks its own copy closed.
func (s *Integrations) CloseServiceNowIncident(ctx context.Context, number string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
// the customer's ServiceNow records. Forward checks every incident for the "still active" refusal (see CloseServiceNowIncident) before it
// closes any, so one active incident means none are closed.
func (s *Integrations) CloseServiceNowIncidents(ctx context.Context, numbers []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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

func checkIncidentsPath(snapshotID string, rest ...string) (string, error) {
	if len(rest) == 0 {
		return checksPath(snapshotID)
	}
	return checkPath(snapshotID, rest[0])
}

// ListCheckIncidents lists the ServiceNow incidents linked to the snapshot's active checks of the given types (GET
// /snapshots/{id}/checks?view=serviceNowIncidents&type=...). At least one type is required; Forward answers 400 for a name it does not know.
func (s *Integrations) ListCheckIncidents(ctx context.Context, snapshotID string, checkTypes []string) ([]ports.ServiceNowIncidentLink, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := []ports.ServiceNowIncidentLink{}
	resp, err := s.client.doRequired(markOperation(req, "Integrations.ListCheckIncidents"), &out)
	return out, resp, err
}

// AddCheckIncident raises a ServiceNow incident for one failing check and links it (POST /snapshots/{id}/checks/{checkId}
// ?action=addServiceNowIncident). Forward refuses a check that is disabled or not failing in this snapshot and every later one. It creates
// a record in the customer's ServiceNow.
func (s *Integrations) AddCheckIncident(ctx context.Context, snapshotID, checkID string, input ports.ServiceNowIncidentInput) (*ports.ServiceNowIncidentLink, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.ServiceNowIncidentLink)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.AddCheckIncident"), out)
	return out, resp, err
}

// AddCheckIncidents raises incidents for several checks in one call, keyed by check ID (POST /snapshots/{id}/checks
// ?action=addServiceNowIncidents). The same conditions as AddCheckIncident apply to every check.
func (s *Integrations) AddCheckIncidents(ctx context.Context, snapshotID string, inputs map[string]ports.ServiceNowIncidentInput) ([]ports.ServiceNowIncidentLink, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := []ports.ServiceNowIncidentLink{}
	resp, err := s.client.doRequired(markOperation(req, "Integrations.AddCheckIncidents"), &out)
	return out, resp, err
}

// LinkCheckIncident links an incident that already exists in ServiceNow to a check (POST /snapshots/{id}/checks/{checkId}
// ?action=linkServiceNowIncident&incident=NUMBER).
func (s *Integrations) LinkCheckIncident(ctx context.Context, snapshotID, checkID, incidentNumber string) (*ports.ServiceNowIncidentLink, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.checkIncidentAction(ctx, snapshotID, checkID, incidentNumber, "linkServiceNowIncident", "Integrations.LinkCheckIncident", true)
}

// UnlinkCheckIncident removes the link between a check and an incident (POST /snapshots/{id}/checks/{checkId}
// ?action=unlinkServiceNowIncident&incident=NUMBER). It leaves the incident in ServiceNow alone.
func (s *Integrations) UnlinkCheckIncident(ctx context.Context, snapshotID, checkID, incidentNumber string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	_, resp, err := s.checkIncidentAction(ctx, snapshotID, checkID, incidentNumber, "unlinkServiceNowIncident", "Integrations.UnlinkCheckIncident", false)
	return resp, err
}

func (s *Integrations) checkIncidentAction(ctx context.Context, snapshotID, checkID, incidentNumber, action, operation string, wantBody bool) (*ports.ServiceNowIncidentLink, *ports.Response, error) {
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
	out := new(ports.ServiceNowIncidentLink)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// RefreshCheckIncidents re-reads the state of every incident linked to the snapshot's checks from ServiceNow (POST /snapshots/{id}/checks
// ?action=refreshServiceNowIncidents, 204).
func (s *Integrations) RefreshCheckIncidents(ctx context.Context, snapshotID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *Integrations) CheckIncidentUpdateHistory(ctx context.Context, snapshotID, checkID, incidentNumber string) ([]ports.ServiceNowIncidentUpdate, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := []ports.ServiceNowIncidentUpdate{}
	resp, err := s.client.doRequired(markOperation(req, "Integrations.CheckIncidentUpdateHistory"), &out)
	return out, resp, err
}
