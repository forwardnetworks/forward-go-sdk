package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// AuditLogRecord is one audited request (AuditLogView). ImpersonatorID and
// ImpersonatorUsername are set only when a Forward support user acted as
// UserID, and only Forward's own users are shown them; everyone else sees
// just Impersonated.
type AuditLogRecord struct {
	Time                 time.Time  `json:"-"`
	RemoteIP             string     `json:"remoteIp"`
	UserID               Identifier `json:"userId,omitempty"`
	ImpersonatorID       Identifier `json:"impersonatorId,omitempty"`
	ImpersonatorUsername string     `json:"impersonatorUsername,omitempty"`
	Impersonated         bool       `json:"impersonated"`
	HTTPMethod           string     `json:"httpMethod"`
	TargetURI            string     `json:"targetUri"`
	HTTPResponseCode     int        `json:"httpResponseCode"`
}

// UnmarshalJSON reads timestamp as an ISO-8601 instant or as epoch
// milliseconds, since Forward has sent both for Instants across builds.
func (r *AuditLogRecord) UnmarshalJSON(data []byte) error {
	type plain AuditLogRecord
	wire := struct {
		plain
		Timestamp json.RawMessage `json:"timestamp"`
	}{}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	millis, err := decodeEpochMillisOrInstant(wire.Timestamp)
	if err != nil {
		return fmt.Errorf("forward: audit log timestamp: %w", err)
	}
	*r = AuditLogRecord(wire.plain)
	if millis != 0 {
		r.Time = time.UnixMilli(millis).UTC()
	}
	return nil
}

// AuditLogPaging is the page Forward returned: Total counts every record that
// matches the filters, not just this page.
type AuditLogPaging struct {
	Offset int   `json:"offset"`
	Limit  int   `json:"limit"`
	Total  int64 `json:"total"`
}

// AuditLogs is one page of audit records, newest first.
type AuditLogs struct {
	Records []AuditLogRecord `json:"records"`
	Paging  AuditLogPaging   `json:"paging"`
}

// AuditLogListOptions filters and pages AuditLogs.List. Empty fields send no
// parameter. Forward matches RemoteIP and TargetURI as case-insensitive
// PREFIXES (SQL ILIKE 'value%', with % and _ taken literally), and the rest
// exactly. A prefix cannot express "contains" or "ends with": to find one
// route among many, filter on a prefix and the method, then test the records.
type AuditLogListOptions struct {
	// StartTime and EndTime bound the record time, both inclusive; the zero
	// time leaves that side open. There is no default window on this route.
	StartTime time.Time
	EndTime   time.Time
	// RemoteIP is a prefix of the caller's address ("10.1." matches 10.1.x.x).
	RemoteIP string
	// UserID and ImpersonatorID are Forward user IDs, not usernames.
	UserID         string
	ImpersonatorID string
	// HTTPMethod is POST, PUT, PATCH, DELETE or GET (any case).
	HTTPMethod string
	// TargetURI is a prefix of the stored path, which has no /api in front;
	// a leading "/api" is dropped for you, so the SDK's own route strings
	// work. Include the query string only if you mean to match it.
	TargetURI        string
	HTTPResponseCode *int
	// Limit is 1 to 10000 (Forward defaults to 1000); Offset is 0 or more.
	// Zero leaves the parameter out.
	Limit  int
	Offset int
}

// AuditLogMaxLimit is the largest page Forward returns.
const AuditLogMaxLimit = 10_000
