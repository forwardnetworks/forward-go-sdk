package forward

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxErrorBody = 1 << 20

// ErrorKind classifies outage-sensitive Forward failures independently of the
// appliance's human-readable error wording.
type ErrorKind string

const (
	ErrorKindUnknown                           ErrorKind = "unknown"
	ErrorKindCollectionAlreadyInProgress       ErrorKind = "collection-already-in-progress"
	ErrorKindSnapshotNotProcessed              ErrorKind = "snapshot-not-processed"
	ErrorKindNetworkNotFound                   ErrorKind = "network-not-found"
	ErrorKindAuthentication                    ErrorKind = "authentication-failure"
	ErrorKindTrustedCertificateApplyInProgress ErrorKind = "trusted-certificate-apply-in-progress"
)

var (
	ErrCollectionAlreadyInProgress       = errors.New("forward: collection already in progress")
	ErrSnapshotNotProcessed              = errors.New("forward: snapshot not processed")
	ErrNetworkNotFound                   = errors.New("forward: network not found")
	ErrAuthentication                    = errors.New("forward: authentication failed")
	ErrTrustedCertificateApplyInProgress = errors.New("forward: trusted certificate apply already in progress for every supported collector")
)

// UnavailableError preserves the reason a fail-closed client could not be
// configured for a tenant.
type UnavailableError struct {
	NetworkID string
	Cause     error
}

func (e *UnavailableError) Error() string {
	if e == nil {
		return "forward: access unavailable"
	}
	if e.NetworkID == "" {
		return fmt.Sprintf("forward: access unavailable: %v", e.Cause)
	}
	return fmt.Sprintf("forward: access unavailable for network %s: %v", e.NetworkID, e.Cause)
}

func (e *UnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ErrorResponse describes a non-2xx response from the Forward API.
type ErrorResponse struct {
	Response *http.Response
	Kind     ErrorKind `json:"-"`
	Method   string    `json:"httpMethod,omitempty"`
	APIURL   string    `json:"apiUrl,omitempty"`
	Message  string    `json:"message,omitempty"`
	Reason   string    `json:"reason,omitempty"`
	Code     string    `json:"code,omitempty"`

	CompletionType string          `json:"completionType,omitempty"`
	Errors         []NQEQueryError `json:"errors,omitempty"`
	SnapshotID     Identifier      `json:"snapshotId,omitempty"`
	Body           []byte          `json:"-"`
}

// Is lets callers use errors.Is with the stable classification sentinels.
func (e *ErrorResponse) Is(target error) bool {
	if e == nil {
		return false
	}
	switch target {
	case ErrCollectionAlreadyInProgress:
		return e.Kind == ErrorKindCollectionAlreadyInProgress
	case ErrSnapshotNotProcessed:
		return e.Kind == ErrorKindSnapshotNotProcessed
	case ErrNetworkNotFound:
		return e.Kind == ErrorKindNetworkNotFound
	case ErrAuthentication:
		return e.Kind == ErrorKindAuthentication
	case ErrTrustedCertificateApplyInProgress:
		return e.Kind == ErrorKindTrustedCertificateApplyInProgress
	default:
		return false
	}
}

func (e *ErrorResponse) Error() string {
	if e == nil {
		return "forward: API error"
	}
	status := "unknown status"
	if e.Response != nil {
		status = e.Response.Status
	}
	detail := strings.TrimSpace(e.Message)
	if detail == "" {
		detail = strings.TrimSpace(string(e.Body))
	}
	if detail == "" {
		return fmt.Sprintf("forward: API returned %s", status)
	}
	return fmt.Sprintf("forward: API returned %s: %s", status, detail)
}

// IsStatus reports whether err is an ErrorResponse with the supplied HTTP
// status code.
func IsStatus(err error, statusCode int) bool {
	var apiErr *ErrorResponse
	return errors.As(err, &apiErr) && apiErr.Response != nil && apiErr.Response.StatusCode == statusCode
}

// IsErrorKind reports whether err is a classified Forward API error.
func IsErrorKind(err error, kind ErrorKind) bool {
	var apiErr *ErrorResponse
	return errors.As(err, &apiErr) && apiErr.Kind == kind
}

// IsCollectionAlreadyInProgress is the compatibility-friendly spelling used
// by collection orchestrators. It performs no string matching at the call site.
func IsCollectionAlreadyInProgress(err error) bool {
	return errors.Is(err, ErrCollectionAlreadyInProgress)
}

// IsTrustedCertificateApplyInProgress reports whether err is the 409 Forward
// returns from TrustedCertificates.Apply when an apply task is already queued
// or running for every supported collector. Callers should treat this as
// "already underway," not a failure, and must not retry in a way that queues
// a second apply once a collector becomes free.
func IsTrustedCertificateApplyInProgress(err error) bool {
	return errors.Is(err, ErrTrustedCertificateApplyInProgress)
}

func newErrorResponse(resp *http.Response) *ErrorResponse {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	apiErr := &ErrorResponse{Response: resp, Body: body}
	_ = json.Unmarshal(body, apiErr)
	if resp.Request != nil {
		if apiErr.Method == "" {
			apiErr.Method = resp.Request.Method
		}
		if apiErr.APIURL == "" && resp.Request.URL != nil {
			apiErr.APIURL = resp.Request.URL.Path
		}
	}
	if apiErr.Code == "" {
		var aliases struct {
			ErrorCode string `json:"errorCode"`
			Error     string `json:"error"`
		}
		if json.Unmarshal(body, &aliases) == nil {
			if aliases.ErrorCode != "" {
				apiErr.Code = aliases.ErrorCode
			} else {
				apiErr.Code = aliases.Error
			}
		}
	}
	apiErr.Kind = classifyErrorResponse(apiErr)
	return apiErr
}

func classifyErrorResponse(apiErr *ErrorResponse) ErrorKind {
	if apiErr == nil || apiErr.Response == nil {
		return ErrorKindUnknown
	}
	status := apiErr.Response.StatusCode
	if status == http.StatusUnauthorized {
		return ErrorKindAuthentication
	}

	path := apiErr.APIURL
	if path == "" && apiErr.Response.Request != nil && apiErr.Response.Request.URL != nil {
		path = apiErr.Response.Request.URL.Path
	}
	method := apiErr.Method
	if method == "" && apiErr.Response.Request != nil {
		method = apiErr.Response.Request.Method
	}
	detail := normalizedErrorDetail(apiErr.Code, apiErr.Reason, apiErr.Message, string(apiErr.Body))
	if status == http.StatusForbidden &&
		(containsWords(detail, "auth", "fail") || containsWords(detail, "credential") || containsWords(detail, "token")) {
		return ErrorKindAuthentication
	}

	if method == http.MethodPost && path == "/api/collector-tasks" &&
		(status == http.StatusBadRequest || status == http.StatusConflict) &&
		(status == http.StatusConflict ||
			containsWords(detail, "collection", "already") &&
				(strings.Contains(detail, "progress") || strings.Contains(detail, "running") ||
					strings.Contains(detail, "active") || strings.Contains(detail, "underway"))) {
		return ErrorKindCollectionAlreadyInProgress
	}
	if strings.HasPrefix(path, "/api/snapshots/") &&
		(status == http.StatusBadRequest || status == http.StatusConflict || status == http.StatusTooEarly) &&
		(status == http.StatusConflict && requestHasNoQuery(apiErr.Response) ||
			containsWords(detail, "snapshot", "not", "processed") ||
			containsWords(detail, "snapshot", "still", "processing") ||
			strings.Contains(detail, "snapshot processing") ||
			strings.Contains(detail, "snapshot unprocessed")) {
		return ErrorKindSnapshotNotProcessed
	}
	if status == http.StatusNotFound &&
		(containsWords(detail, "network", "not", "found") ||
			containsWords(detail, "network", "does", "not", "exist") ||
			isNetworkResourcePath(path)) {
		return ErrorKindNetworkNotFound
	}
	// TrustedCertificateTaskService.applyCertificates is the only handler on
	// this route that throws ConflictException, and only for action=apply.
	if method == http.MethodPost && path == "/api/trusted-certificates" &&
		status == http.StatusConflict && requestHasQueryValue(apiErr.Response, "action", "apply") {
		return ErrorKindTrustedCertificateApplyInProgress
	}
	return ErrorKindUnknown
}

func requestHasQueryValue(response *http.Response, key, value string) bool {
	if response == nil || response.Request == nil || response.Request.URL == nil {
		return false
	}
	return response.Request.URL.Query().Get(key) == value
}

func requestHasNoQuery(response *http.Response) bool {
	return response != nil && response.Request != nil && response.Request.URL != nil && response.Request.URL.RawQuery == ""
}

func normalizedErrorDetail(parts ...string) string {
	joined := strings.ToLower(strings.Join(parts, " "))
	var out strings.Builder
	out.Grow(len(joined))
	for _, r := range joined {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			out.WriteRune(r)
		} else {
			out.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

func containsWords(detail string, words ...string) bool {
	for _, word := range words {
		if !strings.Contains(detail, word) {
			return false
		}
	}
	return true
}

func isNetworkResourcePath(path string) bool {
	rest := strings.TrimPrefix(path, "/api/networks/")
	if rest == path || strings.Trim(rest, "/") == "" {
		return false
	}
	// A network resource or a collection directly beneath it can only fail its
	// path lookup because the network is absent. Deeper routes may instead refer
	// to a missing child and are not classified without a structured message.
	return len(strings.Split(strings.Trim(rest, "/"), "/")) <= 2
}
