package domain

import (
	"encoding/json"
	"time"
)

// TenableSource is a Tenable Security Center source as Forward stores it (StoredEndHostScanner). MaxPageSize is nil when Forward's
// default (10000) applies, and CollectorID empty when no collector is named. Type is TENABLE in the list; Create and Update set it
// themselves because Forward leaves it out of those answers. CollectionError is Forward's error name from the last attempt, if it failed.
type TenableSource struct {
	Name                 string     `json:"name"`
	Type                 string     `json:"type,omitempty"`
	BaseURL              string     `json:"baseUrl,omitempty"`
	CredentialID         Identifier `json:"credentialId,omitempty"`
	CollectionDisabled   bool       `json:"collectionDisabled,omitempty"`
	DisableSSLValidation bool       `json:"disableSslValidation,omitempty"`
	MaxPageSize          *int       `json:"maxPageSize,omitempty"`
	CollectorID          Identifier `json:"collectorId,omitempty"`
	CollectionError      string     `json:"collectionError,omitempty"`
	LastAttempted        string     `json:"lastAttempted,omitempty"`
}

// TenableSourceRequest creates a source (NewTenableSource). MaxPageSize nil sends nothing, so Forward uses 10000; CollectorID empty
// sends nothing.
type TenableSourceRequest struct {
	Name                 string     `json:"name"`
	BaseURL              string     `json:"baseUrl"`
	CredentialID         Identifier `json:"credentialId"`
	DisableSSLValidation bool       `json:"disableSslValidation,omitempty"`
	CollectionDisabled   bool       `json:"collectionDisabled,omitempty"`
	MaxPageSize          *int       `json:"maxPageSize,omitempty"`
	CollectorID          Identifier `json:"collectorId,omitempty"`
}

// TenableSourcePatch changes a source; nil fields stay as they are (TenableSourcePatch). ClearMaxPageSize and ClearCollector send an
// explicit null, which Forward reads as "back to the default" and "no collector" (they win over the value fields).
type TenableSourcePatch struct {
	Name                 *string
	BaseURL              *string
	CredentialID         *Identifier
	DisableSSLValidation *bool
	CollectionDisabled   *bool
	MaxPageSize          *int
	CollectorID          *Identifier
	ClearMaxPageSize     bool
	ClearCollector       bool
}

// MarshalJSON sends only what is set, with explicit nulls for the two Clear flags.
func (p TenableSourcePatch) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	if p.Name != nil {
		m["name"] = *p.Name
	}
	if p.BaseURL != nil {
		m["baseUrl"] = *p.BaseURL
	}
	if p.CredentialID != nil {
		m["credentialId"] = *p.CredentialID
	}
	if p.DisableSSLValidation != nil {
		m["disableSslValidation"] = *p.DisableSSLValidation
	}
	if p.CollectionDisabled != nil {
		m["collectionDisabled"] = *p.CollectionDisabled
	}
	switch {
	case p.ClearMaxPageSize:
		m["maxPageSize"] = nil
	case p.MaxPageSize != nil:
		m["maxPageSize"] = *p.MaxPageSize
	}
	switch {
	case p.ClearCollector:
		m["collectorId"] = nil
	case p.CollectorID != nil:
		m["collectorId"] = *p.CollectorID
	}
	return json.Marshal(m)
}

// TenableSourceTest is what a connection test needs (EndHostScannerTestConfig): the same fields as a source, none of them stored.
type TenableSourceTest struct {
	Name                 string     `json:"name"`
	BaseURL              string     `json:"baseUrl"`
	CredentialID         Identifier `json:"credentialId"`
	DisableSSLValidation bool       `json:"disableSslValidation,omitempty"`
	CollectorID          Identifier `json:"collectorId,omitempty"`
}

// SourceTestResult is the verdict of a connection test (EndHostScannerTestResult). Forward leaves Error out on success.
type SourceTestResult struct {
	Error string `json:"error,omitempty"`
}

// OK reports whether the test found nothing wrong.
func (r SourceTestResult) OK() bool { return r.Error == "" }

// TenableTestTimeout is the HTTP timeout TestTenableSC needs: Forward holds the request until the collector finishes, which can take
// its full 60 seconds (TestEndHostScannerTaskHandler.TEST_END_HOST_SCANNER_OP_TIMEOUT) plus a margin.
const TenableTestTimeout = 90 * time.Second
