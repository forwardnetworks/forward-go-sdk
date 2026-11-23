package domain

import (
	"errors"
	"fmt"
)

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
	ErrorKindEndpointProfileInUse ErrorKind = "endpoint-profile-in-use"
	// ErrorKindPermissionDenied is a 403 for a missing org or network operation ("Missing permission: OrgOperation.X" or "No
	// permission for network operation X"); ErrorResponse.Operation names it.
	ErrorKindPermissionDenied ErrorKind = "permission-denied"
	// ErrorKindRateLimited is Forward's per-user API rate limiter (RateLimitService; OFF unless the deployment enables API_RATE_LIMITING, an on-premises setting): the user sent more than
	// MAX_API_REQ_PER_USER_PER_MIN requests in a minute (default 2000) and is LOCKED OUT, every session and client
	// sharing that login, for API_RATE_LIMITER_LOCKOUT_MINUTES (default 1, at most 60). The 429 carries the remaining
	// lockout in Retry-After (RateLimitLockout reads it). Not every 429 is this: Forward's one-active-chat 429 is not.
	ErrorKindRateLimited ErrorKind = "rate-limited"
	// ErrorKindUnlicensedOperation is a 403 for an operation the org's license tier does not include ("Unlicensed operation: ...") --
	// a license problem, not a role one.
	ErrorKindUnlicensedOperation ErrorKind = "unlicensed-operation"
	// ErrorKindEndpointNotServed is a 404 because Forward has no route at all for this method and path on this
	// deployment: Spring's NoHandlerFoundException, which Forward renders as the JSON message "No endpoint <METHOD> <URL>."
	// with no reason code. It is what a controller excluded by a deployment profile looks like (the org licence routes are
	// SaaS-only, the backup and restore routes on-prem Kubernetes only), and also what a mistyped path or a route from a
	// newer Forward looks like, so it means "not served here", never "the thing you asked for is missing". Contrast
	// ErrorKindNetworkNotFound, a route that exists and found no such network.
	ErrorKindEndpointNotServed ErrorKind = "endpoint-not-served"
	ErrorKindNetworkNotFound   ErrorKind = "network-not-found"
	// ErrorKindServiceNowInstanceAsleep is a 409 whose message is "ServiceNow instance is hibernating" or "ServiceNow instance is suspended":
	// the customer's ServiceNow developer or trial instance is not running (Forward's ServiceNowApiUtils.classifyOkBody reads ServiceNow's
	// 200 wake-up page and refuses the call). Nothing was created. Retrying later, once the instance is woken, can work; the 409 "Unknown
	// Error" a woken instance answered live is NOT this kind (its cause is unconfirmed).
	ErrorKindServiceNowInstanceAsleep ErrorKind = "servicenow-instance-asleep"
	// ErrorKindActionNotSupported is a 400 for a ?action= (or other query-parameter condition) this deployment does not map on the
	// route: Spring's UnsatisfiedServletRequestParameterException, `Parameter conditions "action=bulkAdd" not met for actual request
	// parameters: ...`. The route exists but not that variant, which is what an older build looks like for a newer action, so a caller
	// can fall back to the older call (StageBGPAdvertisements to StageBGPAdvertisement) without matching Forward's text itself.
	ErrorKindActionNotSupported ErrorKind = "action-not-supported"
	ErrorKindAuthentication     ErrorKind = "authentication-failure"
	// ErrorKindAccountLocked is a 401 because the login or access key is locked out after repeated failed attempts, not because
	// this request's credentials were wrong (UserAuthService / ApiTokenAuthenticationProvider: "User account is locked: <name>",
	// "Account is locked"). Forward counts failed attempts and locks whatever the deployment flags (after every third failure: 1, 5, 15
	// minutes, then 1 and 3 hours); API_RATE_LIMITING only decides whether a locked identifier is refused before its credentials are
	// checked. The lock is per identifier and carries no Retry-After, so its length is unknown. A locked 401 matches ErrAuthentication,
	// which every 401 does, but IsErrorKind(err, ErrorKindAuthentication) is false for it. Never retried: a 401 is not.
	ErrorKindAccountLocked                     ErrorKind = "account-locked"
	ErrorKindTrustedCertificateApplyInProgress ErrorKind = "trusted-certificate-apply-in-progress"
	// ErrorKindUnknownOrgProperty is the appserver refusing an org property
	// NAME its OrgProperty enum does not define (Spring's 400 "No enum constant
	// com.forwardnetworks.cv.config.OrgProperty.X"). Property sets are
	// version-dependent, so a caller writing across releases needs to tell this
	// apart from a real failure without matching prose.
	ErrorKindUnknownOrgProperty ErrorKind = "unknown-org-property"
	// ErrorKindUnknownNetworkProperty is the same refusal for a NETWORK property name (Spring's 400 "No enum constant
	// com.forwardnetworks.cv.config.NetworkProperty.X"): Forward defines far fewer network properties than org properties, so an org-only
	// property such as LOCATION_CONNECTIVITY_DIFFS written per network answers this (seen live on 26.10.6).
	ErrorKindUnknownNetworkProperty ErrorKind = "unknown-network-property"
	// ErrorKindCheckFieldNotAllowed is the 400 Forward answers when a check create carries a name, note, tags or perfMonitoringEnabled the
	// check type does not take, or lacks a name it requires ("'name' not allowed for non-parameterized NQE checks", "'name' required for
	// parameterized NQE checks", "'note' not allowed for NQE checks", "'tags' not allowed for NQE checks"; the not-allowed forms for NQE were
	// seen live on 26.10.6 by the AutoCon6 workshop, the rest are from NewNetworkCheck.java). See ValidateNewCheck for the rules.
	ErrorKindCheckFieldNotAllowed ErrorKind = "check-field-not-allowed"
	// ErrorKindUnrecognizedDevices is the 400 "Unrecognized devices cannot be moved: [r1, r2]" Locations.Assign gets, for the WHOLE patch,
	// when any device in it is not one the network's classic-device list (or atlas) knows, even a device present in every snapshot (a network
	// fed only by snapshot uploads). Seen live on 26.10.6 by the AutoCon6 workshop; what worked there was creating each device with
	// ClassicDevices.Create (collect=false, no credentials) and assigning again. ErrorResponse.UnrecognizedDevices lists them.
	ErrorKindUnrecognizedDevices ErrorKind = "unrecognized-devices"
	// ErrorKindSnapshotMergeConflict is the 409 "Conflicting devices" Snapshots.MergeIntoLatest gets with mergeType NONE when a device is in
	// both the uploaded ZIP and the latest snapshot (SnapshotController.mergeAndCreateSnapshot; from source, not seen live).
	// ErrorResponse.ConflictDevices lists them, sorted.
	ErrorKindSnapshotMergeConflict ErrorKind = "snapshot-merge-conflict"
	// ErrorKindOrgNotAccessible is the 403 "Org <id> is not accessible to user <id>" a support or cross-org key gets for an organization that
	// has not granted it access (seen live on 26.10.6 with a support-org key on /api/orgs/{id}/config).
	ErrorKindOrgNotAccessible ErrorKind = "org-not-accessible"
	// ErrorKindGlobalDefaultNotAllowed is the 403 "Permission denied. Global default value cannot be changed for <property>" a global write
	// gets for a property that can only be set per org (its Level is ORG, not BOTH). ErrorResponse.GateProperty carries the property name.
	ErrorKindGlobalDefaultNotAllowed ErrorKind = "global-default-not-allowed"
)

var (
	ErrCollectionAlreadyInProgress       = errors.New("forward: collection already in progress")
	ErrSnapshotNotProcessed              = errors.New("forward: snapshot not processed")
	ErrSnapshotProcessingFailed          = errors.New("forward: snapshot processing failed")
	ErrFeatureGated                      = errors.New("forward: feature not available for this organization or deployment")
	ErrEndpointProfileInUse              = errors.New("forward: endpoint profile is still used by endpoints")
	ErrPermissionDenied                  = errors.New("forward: missing permission for the operation")
	ErrAccountLocked                     = errors.New("forward: account is locked out after repeated failed authentication")
	ErrRateLimited                       = errors.New("forward: API rate limit exceeded, the user is locked out for a while")
	ErrUnlicensedOperation               = errors.New("forward: operation not included in the organization's license")
	ErrEndpointNotServed                 = errors.New("forward: route is not served by this deployment")
	ErrActionNotSupported                = errors.New("forward: this deployment does not map that action on the route")
	ErrServiceNowInstanceAsleep          = errors.New("forward: the ServiceNow instance is hibernating or suspended")
	ErrNetworkNotFound                   = errors.New("forward: network not found")
	ErrAuthentication                    = errors.New("forward: authentication failed")
	ErrTrustedCertificateApplyInProgress = errors.New("forward: trusted certificate apply already in progress for every supported collector")
	ErrUnknownOrgProperty                = errors.New("forward: organization property not defined by this appserver")
	ErrUnknownNetworkProperty            = errors.New("forward: network property not defined by this appserver")
	ErrOrgNotAccessible                  = errors.New("forward: the organization is not accessible to this user")
	ErrCheckFieldNotAllowed              = errors.New("forward: the check type does not take that field")
	ErrUnrecognizedDevices               = errors.New("forward: the network does not know some of the devices named")
	ErrSnapshotMergeConflict             = errors.New("forward: devices are in both the uploaded snapshot and the latest one")
	ErrGlobalDefaultNotAllowed           = errors.New("forward: the property can only be set per organization, not as a global default")
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

// IsGlobalDefaultNotAllowed reports whether err is Forward's 403 for a global default write to a property that is only settable per
// organization (see Properties.SetGlobal).
func IsGlobalDefaultNotAllowed(err error) bool {
	return errors.Is(err, ErrGlobalDefaultNotAllowed)
}

// IsUnknownOrgProperty reports whether err is the appserver's 400 for an org
// property name its enum does not define.
func IsUnknownOrgProperty(err error) bool {
	return errors.Is(err, ErrUnknownOrgProperty)
}

// IsUnknownNetworkProperty reports whether err is the 400 for a network property name the appserver does not define.
func IsUnknownNetworkProperty(err error) bool { return errors.Is(err, ErrUnknownNetworkProperty) }

// IsOrgNotAccessible reports whether err is the 403 for an organization the caller has not been granted access to.
func IsOrgNotAccessible(err error) bool { return errors.Is(err, ErrOrgNotAccessible) }
