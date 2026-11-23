package ports

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

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
	// For ErrorKindPermissionDenied and ErrorKindUnlicensedOperation: the operation Forward named, e.g.
	// "OrgOperation.MANAGE_USER_ACCOUNTS" or, from the network-operation wording, "EDIT_CHECKS".
	Operation string `json:"-"`

	GateProperty string `json:"-"`
	// UnrecognizedDevices names the devices of an ErrorKindUnrecognizedDevices refusal, in the order Forward listed them.
	UnrecognizedDevices []string `json:"-"`
	// ConflictDevices names the devices of an ErrorKindSnapshotMergeConflict refusal (the body's conflictDevices), sorted by Forward.
	ConflictDevices []string `json:"conflictDevices,omitempty"`
	GateEnabled     bool     `json:"-"`
	GateScope       string   `json:"-"`
	Body            []byte   `json:"-"`
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
	case ErrPermissionDenied:
		return e.Kind == ErrorKindPermissionDenied
	case ErrRateLimited:
		return e.Kind == ErrorKindRateLimited
	case ErrUnlicensedOperation:
		return e.Kind == ErrorKindUnlicensedOperation
	case ErrEndpointNotServed:
		return e.Kind == ErrorKindEndpointNotServed
	case ErrActionNotSupported:
		return e.Kind == ErrorKindActionNotSupported
	case ErrServiceNowInstanceAsleep:
		return e.Kind == ErrorKindServiceNowInstanceAsleep
	case ErrNetworkNotFound:
		return e.Kind == ErrorKindNetworkNotFound
	case ErrAuthentication:
		return e.Kind == ErrorKindAuthentication || e.Kind == ErrorKindAccountLocked
	case ErrAccountLocked:
		return e.Kind == ErrorKindAccountLocked
	case ErrTrustedCertificateApplyInProgress:
		return e.Kind == ErrorKindTrustedCertificateApplyInProgress
	case ErrUnknownOrgProperty:
		return e.Kind == ErrorKindUnknownOrgProperty
	case ErrUnknownNetworkProperty:
		return e.Kind == ErrorKindUnknownNetworkProperty
	case ErrOrgNotAccessible:
		return e.Kind == ErrorKindOrgNotAccessible
	case ErrCheckFieldNotAllowed:
		return e.Kind == ErrorKindCheckFieldNotAllowed
	case ErrUnrecognizedDevices:
		return e.Kind == ErrorKindUnrecognizedDevices
	case ErrSnapshotMergeConflict:
		return e.Kind == ErrorKindSnapshotMergeConflict
	case ErrGlobalDefaultNotAllowed:
		return e.Kind == ErrorKindGlobalDefaultNotAllowed
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

// MissingPermission reports the operation a 403 says the caller lacks -- "OrgOperation.MANAGE_USER_ACCOUNTS",
// "NetworkOperation.EDIT_CHECKS", or a bare "EDIT_CHECKS" from the network-operation wording -- and false for any other error,
// including an unlicensed operation (see UnlicensedOperation).
func MissingPermission(err error) (operation string, ok bool) {
	var apiErr *ErrorResponse
	if errors.As(err, &apiErr) && apiErr.Kind == ErrorKindPermissionDenied {
		return apiErr.Operation, true
	}
	return "", false
}

// UnlicensedOperation reports the operation a 403 says the organization's license does not include, and false for any other error.
func UnlicensedOperation(err error) (operation string, ok bool) {
	var apiErr *ErrorResponse
	if errors.As(err, &apiErr) && apiErr.Kind == ErrorKindUnlicensedOperation {
		return apiErr.Operation, true
	}
	return "", false
}

// RateLimitLockout reports the lockout Forward's API rate limiter imposed, as
// the Retry-After of its 429, and false for any other error. The lockout is for
// the whole user, so every client sharing the login is refused until it ends;
// requests made during it do not extend it (RateLimitService answers from the
// existing block before counting), but retrying sooner is pointless.
func RateLimitLockout(err error) (time.Duration, bool) {
	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrorKindRateLimited || apiErr.Response == nil {
		return 0, false
	}
	return retryAfter(apiErr.Response.Header.Get("Retry-After"), time.Now())
}

// IsGone reports whether err is Forward's 404 for a resource that is not there: a delete that finds nothing to delete has
// nothing left to do. A 404 that means the deployment does not serve the route (ErrEndpointNotServed) is not gone.
func IsGone(err error) bool {
	return IsStatus(err, http.StatusNotFound) && !errors.Is(err, ErrEndpointNotServed)
}
