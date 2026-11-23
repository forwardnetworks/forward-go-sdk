package domain

// ServiceNow impact levels of a new change request (Forward's ServiceNowChangeRequestLevel names).
const (
	ServiceNowImpactHigh   = "HIGH"
	ServiceNowImpactMedium = "MEDIUM"
	ServiceNowImpactLow    = "LOW"
)

// ServiceNow priorities of a new change request (Forward's ServiceNowPriorityLevel names).
const (
	ServiceNowPriorityCritical = "CRITICAL"
	ServiceNowPriorityHigh     = "HIGH"
	ServiceNowPriorityModerate = "MODERATE"
	ServiceNowPriorityLow      = "LOW"
)

// Limits Forward enforces on a change request attachment.
const (
	ServiceNowMaxAttachmentBytes    = 25 << 20
	ServiceNowMaxAttachmentFileName = 100
)

// ServiceNowChangeRequestInput is a change request to raise (Forward's NewServiceNowChangeRequest). Summary and Impact are required;
// Details, Priority and WorkNotes are left out when empty.
type ServiceNowChangeRequestInput struct {
	Summary   string `json:"summary"`
	Details   string `json:"details,omitempty"`
	Impact    string `json:"impact"`
	Priority  string `json:"priority,omitempty"`
	WorkNotes string `json:"workNotes,omitempty"`
}

// ServiceNowAttachment is a file attached to a change request.
type ServiceNowAttachment struct {
	FileName  string `json:"fileName"`
	FileSysID string `json:"fileSysId"`
}

// ServiceNowChangeRequest is a change request as Forward returns it. Impact and Priority come back in ServiceNow's display form
// ("2 - Medium"), not the names the input takes; State is ServiceNow's label (New, Assess, Authorize, Scheduled, Implement, Review, Closed,
// Canceled) and is empty when ServiceNow sent none Forward knows. URL is the request's page in ServiceNow.
type ServiceNowChangeRequest struct {
	Number      string                 `json:"number"`
	SysID       string                 `json:"sysId"`
	Summary     string                 `json:"summary,omitempty"`
	Details     string                 `json:"details,omitempty"`
	Impact      string                 `json:"impact"`
	Priority    string                 `json:"priority,omitempty"`
	State       string                 `json:"state,omitempty"`
	URL         string                 `json:"url"`
	Attachments []ServiceNowAttachment `json:"attachments,omitempty"`
}
