package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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
type AuditLogs service

// List returns audit records, newest first. GET /api/audit-logs
// (AuditLogController.getAuditLogs; VIEW_AUDIT_LOGS, or a Forward admin).
func (s *AuditLogs) List(ctx context.Context, options ports.AuditLogListOptions) (*ports.AuditLogs, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if options.Limit < 0 || options.Limit > ports.AuditLogMaxLimit {
		return nil, nil, fmt.Errorf("forward: audit log limit must be between 1 and %d", ports.AuditLogMaxLimit)
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
	out := new(ports.AuditLogs)
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
