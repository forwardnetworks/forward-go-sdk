package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AuditLogsService reads Forward's audit log: which signed-in user called
// which mutating route, from where, when, and with what response code. It
// records requests, not bodies, so it says who did something and when, never
// what the change was. AuditLogController, served on primary 15398425a69 and
// stable 67e89c87124. Preview: not in the published spec.
//
// What is recorded (RequestLoggingFilter.persistAuditLogRecord):
//   - POST, PUT, PATCH and DELETE requests. GETs are recorded only on the few
//     routes annotated @Audited, and a mutating route can opt out the same
//     way (e.g. schema inference and connector tests), so absence is not proof
//     nothing happened.
//   - Only authenticated users. Collector requests are not recorded, and
//     failed logins go to a separate record.
//   - The path after /api -- "/networks/123/classic-devices", not
//     "/api/networks/123/classic-devices" -- plus the query string, with
//     _csrf, password, token and secret values replaced by "<redacted>",
//     cut at 2048 characters.
type AuditLogsService service

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

// List returns audit records, newest first. GET /api/audit-logs
// (AuditLogController.getAuditLogs; VIEW_AUDIT_LOGS, or a Forward admin).
func (s *AuditLogsService) List(ctx context.Context, options AuditLogListOptions) (*AuditLogs, *Response, error) {
	if options.Limit < 0 || options.Limit > AuditLogMaxLimit {
		return nil, nil, fmt.Errorf("forward: audit log limit must be between 1 and %d", AuditLogMaxLimit)
	}
	if options.Offset < 0 {
		return nil, nil, errors.New("forward: audit log offset must not be negative")
	}
	if !options.StartTime.IsZero() && !options.EndTime.IsZero() && options.EndTime.Before(options.StartTime) {
		return nil, nil, errors.New("forward: audit log end time is before its start time")
	}
	query := url.Values{}
	if !options.StartTime.IsZero() {
		query.Set("startTime", options.StartTime.UTC().Format(time.RFC3339Nano))
	}
	if !options.EndTime.IsZero() {
		query.Set("endTime", options.EndTime.UTC().Format(time.RFC3339Nano))
	}
	setString(query, "remoteIp", options.RemoteIP)
	setString(query, "userId", options.UserID)
	setString(query, "impersonatorId", options.ImpersonatorID)
	if method := strings.ToUpper(strings.TrimSpace(options.HTTPMethod)); method != "" {
		switch method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodGet:
			query.Set("httpMethod", method)
		default:
			return nil, nil, fmt.Errorf("forward: audit logs record POST, PUT, PATCH, DELETE and GET, not %q", options.HTTPMethod)
		}
	}
	setString(query, "targetUri", auditTargetURI(options.TargetURI))
	if options.HTTPResponseCode != nil {
		query.Set("httpResponseCode", strconv.Itoa(*options.HTTPResponseCode))
	}
	if options.Limit != 0 {
		query.Set("limit", strconv.Itoa(options.Limit))
	}
	if options.Offset != 0 {
		query.Set("offset", strconv.Itoa(options.Offset))
	}
	path := "/api/audit-logs"
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "AuditLogs.List")
	out := new(AuditLogs)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// auditTargetURI drops a leading "/api" (the servlet path Forward does not
// store) so a route copied from the SDK or the API docs matches.
func auditTargetURI(target string) string {
	target = strings.TrimSpace(target)
	if target == "/api" {
		return ""
	}
	if strings.HasPrefix(target, "/api/") {
		return target[len("/api"):]
	}
	return target
}
