package domain

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
