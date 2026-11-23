package domain

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
