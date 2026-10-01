package forward

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const maxErrorBody = 1 << 20

// ErrorKind classifies outage-sensitive Forward failures independently of the
// appliance's human-readable error wording.
type ErrorKind string

const (
	ErrorKindUnknown                     ErrorKind = "unknown"
	ErrorKindCollectionAlreadyInProgress ErrorKind = "collection-already-in-progress"
	ErrorKindSnapshotNotProcessed        ErrorKind = "snapshot-not-processed"
	// ErrorKindSnapshotProcessingFailed is a snapshot whose processing ended
	// badly: failed, canceled or timed out. Unlike SnapshotNotProcessed,
	// waiting does not help -- the snapshot has to be reprocessed or replaced.
	ErrorKindSnapshotProcessingFailed ErrorKind = "snapshot-processing-failed"
	// ErrorKindNoSnapshots is a snapshot-scoped call made without a snapshot
	// ID on a network that has no snapshot Forward can choose (none at all,
	// or only drafts and Predict forks).
	ErrorKindNoSnapshots ErrorKind = "no-snapshots"
	// ErrorKindFeatureGated is a route refused because an org or deployment
	// property is not set the way the route requires -- a feature switched off
	// (or, rarely, on) for this tenant, not a missing permission.
	// ErrorResponse.GateProperty names the property.
	ErrorKindFeatureGated ErrorKind = "feature-gated"
	// ErrorKindEndpointProfileInUse is Forward refusing to delete an endpoint profile that endpoints still use
	// (NetworkEndpointService.deleteProfile); reassign them first.
	ErrorKindEndpointProfileInUse              ErrorKind = "endpoint-profile-in-use"
	ErrorKindNetworkNotFound                   ErrorKind = "network-not-found"
	ErrorKindAuthentication                    ErrorKind = "authentication-failure"
	ErrorKindTrustedCertificateApplyInProgress ErrorKind = "trusted-certificate-apply-in-progress"
	// ErrorKindUnknownOrgProperty is the appserver refusing an org property
	// NAME its OrgProperty enum does not define (Spring's 400 "No enum constant
	// com.forwardnetworks.cv.config.OrgProperty.X"). Property sets are
	// version-dependent, so a caller writing across releases needs to tell this
	// apart from a real failure without matching prose.
	ErrorKindUnknownOrgProperty ErrorKind = "unknown-org-property"
)

var (
	ErrCollectionAlreadyInProgress       = errors.New("forward: collection already in progress")
	ErrSnapshotNotProcessed              = errors.New("forward: snapshot not processed")
	ErrSnapshotProcessingFailed          = errors.New("forward: snapshot processing failed")
	ErrFeatureGated                      = errors.New("forward: feature not available for this organization or deployment")
	ErrEndpointProfileInUse              = errors.New("forward: endpoint profile is still used by endpoints")
	ErrNetworkNotFound                   = errors.New("forward: network not found")
	ErrAuthentication                    = errors.New("forward: authentication failed")
	ErrTrustedCertificateApplyInProgress = errors.New("forward: trusted certificate apply already in progress for every supported collector")
	ErrUnknownOrgProperty                = errors.New("forward: organization property not defined by this appserver")
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
	// SnapshotState accompanies reason SNAPSHOT_UNAVAILABLE: the state the
	// refused snapshot is in (UNPROCESSED, PROCESSING, ...).
	SnapshotState string `json:"snapshotState,omitempty"`
	// LatestSnapshotID and LatestSnapshotState accompany reason
	// NO_QUALIFYING_SNAPSHOT when the network does have a snapshot, just not a
	// processed one.
	LatestSnapshotID    Identifier `json:"latestSnapshotId,omitempty"`
	LatestSnapshotState string     `json:"latestSnapshotState,omitempty"`

	// For ErrorKindFeatureGated: the OrgProperty or DeploymentProperty that
	// gated the route (e.g. LOCATION_CONNECTIVITY_DIFFS), its current value,
	// and GateScope "organization" or "deployment".
	GateProperty string `json:"-"`
	GateEnabled  bool   `json:"-"`
	GateScope    string `json:"-"`
	Body         []byte `json:"-"`
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
	case ErrSnapshotProcessingFailed:
		return e.Kind == ErrorKindSnapshotProcessingFailed
	case ErrNoSnapshots:
		return e.Kind == ErrorKindNoSnapshots
	case ErrFeatureGated:
		return e.Kind == ErrorKindFeatureGated
	case ErrEndpointProfileInUse:
		return e.Kind == ErrorKindEndpointProfileInUse
	case ErrNetworkNotFound:
		return e.Kind == ErrorKindNetworkNotFound
	case ErrAuthentication:
		return e.Kind == ErrorKindAuthentication
	case ErrTrustedCertificateApplyInProgress:
		return e.Kind == ErrorKindTrustedCertificateApplyInProgress
	case ErrUnknownOrgProperty:
		return e.Kind == ErrorKindUnknownOrgProperty
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

// IsUnknownOrgProperty reports whether err is the appserver's 400 for an org
// property name its enum does not define.
func IsUnknownOrgProperty(err error) bool {
	return errors.Is(err, ErrUnknownOrgProperty)
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
	// Checked before the authentication heuristics below: a property name can
	// contain TOKEN or CREDENTIAL, and a feature switched off is not a login
	// problem.
	if status == http.StatusForbidden {
		if match := featureGateMessage.FindStringSubmatch(strings.TrimSpace(apiErr.Message)); match != nil {
			apiErr.GateProperty, apiErr.GateEnabled, apiErr.GateScope = match[1], match[2] == "on", match[3]
			return ErrorKindFeatureGated
		}
	}
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
	// Forward's own reason codes, on any snapshot-scoped route (NQE, paths,
	// diffs, ...): SnapshotProcessingInterceptor guards every @RequiresSnapshot
	// handler, and ForwardHandlerExceptionResolver renders its refusals.
	if status == http.StatusConflict {
		switch apiErr.Reason {
		case "SNAPSHOT_UNAVAILABLE":
			return ErrorKindSnapshotNotProcessed
		case "NO_QUALIFYING_SNAPSHOT":
			if apiErr.LatestSnapshotID != "" {
				return ErrorKindSnapshotNotProcessed
			}
			return ErrorKindNoSnapshots
		}
	}
	// FailedSnapshotAccessException, ProcessingCanceledException and
	// ProcessingTimedOutException are 400s with no reason code, so the fixed
	// texts ForwardHandlerExceptionResolver writes are all there is to match.
	if status == http.StatusBadRequest &&
		(strings.Contains(detail, "snapshot you are attempting to access failed to process") ||
			strings.Contains(detail, "processing was canceled for snapshot") ||
			strings.Contains(detail, "processing timed out for snapshot")) {
		return ErrorKindSnapshotProcessingFailed
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
	if status == http.StatusBadRequest &&
		strings.Contains(detail, "no enum constant") && strings.Contains(detail, "orgproperty") {
		return ErrorKindUnknownOrgProperty
	}
	// Requests.validate in NetworkEndpointService.deleteProfile: "Profile is still used in %s network(s): %s".
	if method == http.MethodDelete && status == http.StatusBadRequest && strings.HasPrefix(path, "/api/endpoint-profiles/") &&
		strings.Contains(detail, "profile is still used in") {
		return ErrorKindEndpointProfileInUse
	}
	if method == http.MethodPost && path == "/api/trusted-certificates" &&
		status == http.StatusConflict && requestHasQueryValue(apiErr.Response, "action", "apply") {
		return ErrorKindTrustedCertificateApplyInProgress
	}
	return ErrorKindUnknown
}

// featureGateMessage is the whole message AccessEnforcer's property checks
// produce (PermissionsErrorMessages: "%s is %s for your organization" and
// "... for your deployment") -- a 403 with no reason code, so this fixed
// template is the only stable signal. It is anchored so only that exact
// sentence classifies, never a message that merely contains it.
var featureGateMessage = regexp.MustCompile(`^([A-Z][A-Z0-9_]*) is (on|off) for your (organization|deployment)$`)

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
