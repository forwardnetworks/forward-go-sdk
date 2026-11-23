package rest

import (
	"encoding/json"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const maxErrorBody = 1 << 20

func newErrorResponse(resp *http.Response) *ports.ErrorResponse {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	apiErr := &ports.ErrorResponse{Response: resp, Body: body}
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

var globalDefaultMessage = regexp.MustCompile(`(?i)^Permission denied\. Global default value cannot be changed for ([A-Za-z][A-Za-z0-9_]*)$`)

func classifyErrorResponse(apiErr *ports.ErrorResponse) ports.ErrorKind {
	if apiErr == nil || apiErr.Response == nil {
		return ports.ErrorKindUnknown
	}
	status := apiErr.Response.StatusCode
	if status == http.StatusUnauthorized {
		// The form login's JsonAuthenticationFailureHandler answers {"error":"locked"} with no message.
		if message := strings.TrimSpace(apiErr.Message); strings.HasPrefix(message, "User account is locked") || message == "Account is locked" || apiErr.Code == "locked" {
			return ports.ErrorKindAccountLocked
		}
		return ports.ErrorKindAuthentication
	}

	path := apiErr.APIURL
	if path == "" && apiErr.Response.Request != nil && apiErr.Response.Request.URL != nil {
		path = apiErr.Response.Request.URL.Path
	}
	// The appserver's apiUrl is RequestUtils.getRequestString: the URI WITH its query string
	// ("/api/collector-tasks?networkId=5&type=NETWORK_COLLECTION"), so the routes below must be
	// matched on the path alone or no real response ever equals one.
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
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
		message := strings.TrimSpace(apiErr.Message)
		// AccessEnforcer: "Missing permission: %s.%s" and "Unlicensed operation: %s.%s"; UserPrincipal.verifyCan: "No permission
		// for network operation %s".
		if match := permissionMessage.FindStringSubmatch(message); match != nil {
			apiErr.Operation = match[2]
			if match[1] == "Unlicensed operation" {
				return ports.ErrorKindUnlicensedOperation
			}
			return ports.ErrorKindPermissionDenied
		}
		// ConfigController.verifyGlobalConfigPermitted: "Permission denied. Global default value cannot be changed for <property>".
		if match := globalDefaultMessage.FindStringSubmatch(message); match != nil {
			apiErr.GateProperty = strings.ToUpper(match[1])
			return ports.ErrorKindGlobalDefaultNotAllowed
		}
		if match := featureGateMessage.FindStringSubmatch(message); match != nil {
			apiErr.GateProperty, apiErr.GateEnabled, apiErr.GateScope = match[1], match[2] == "on", match[3]
			return ports.ErrorKindFeatureGated
		}
	}
	if status == http.StatusBadRequest {
		if match := unrecognizedDevicesMessage.FindStringSubmatch(strings.TrimSpace(apiErr.Message)); match != nil {
			apiErr.UnrecognizedDevices = nil
			for _, name := range strings.Split(match[1], ",") {
				if name = strings.TrimSpace(name); name != "" {
					apiErr.UnrecognizedDevices = append(apiErr.UnrecognizedDevices, name)
				}
			}
			return ports.ErrorKindUnrecognizedDevices
		}
	}
	if status == http.StatusBadRequest && checkFieldNotAllowedMessage.MatchString(strings.TrimSpace(apiErr.Message)) {
		return ports.ErrorKindCheckFieldNotAllowed
	}
	// TaskPermissionsService: AccessDeniedException("Org %s is not accessible to user %s").
	if status == http.StatusForbidden && orgNotAccessibleMessage.MatchString(strings.TrimSpace(apiErr.Message)) {
		return ports.ErrorKindOrgNotAccessible
	}
	// RateDetectionFilter / SessionRateLimitFilter: 429 "Rate limit exceeded, please try again later".
	if status == http.StatusTooManyRequests && strings.HasPrefix(strings.TrimSpace(apiErr.Message), "Rate limit exceeded") {
		return ports.ErrorKindRateLimited
	}
	// SnapshotController.mergeAndCreateSnapshot: ConflictException("Conflicting devices", {conflictDevices: [...]}) on action=merge.
	if status == http.StatusConflict && requestHasQueryValue(apiErr.Response, "action", "merge") &&
		(len(apiErr.ConflictDevices) != 0 || strings.EqualFold(strings.TrimSpace(apiErr.Message), "Conflicting devices")) {
		return ports.ErrorKindSnapshotMergeConflict
	}
	if status == http.StatusConflict && serviceNowAsleepMessage.MatchString(strings.TrimSpace(apiErr.Message)) {
		return ports.ErrorKindServiceNowInstanceAsleep
	}
	if status == http.StatusBadRequest && strings.Contains(detail, "parameter conditions") && strings.Contains(detail, "not met") {
		return ports.ErrorKindActionNotSupported
	}
	if status == http.StatusForbidden &&
		(containsWords(detail, "auth", "fail") || containsWords(detail, "credential") || containsWords(detail, "token")) {
		return ports.ErrorKindAuthentication
	}

	if method == http.MethodPost && path == "/api/collector-tasks" &&
		(status == http.StatusBadRequest || status == http.StatusConflict) &&
		(status == http.StatusConflict ||
			containsWords(detail, "collection", "already") &&
				(strings.Contains(detail, "progress") || strings.Contains(detail, "running") ||
					strings.Contains(detail, "active") || strings.Contains(detail, "underway"))) {
		return ports.ErrorKindCollectionAlreadyInProgress
	}
	// Forward's own reason codes, on any snapshot-scoped route (NQE, paths,
	// diffs, ...): SnapshotProcessingInterceptor guards every @RequiresSnapshot
	// handler, and ForwardHandlerExceptionResolver renders its refusals.
	if status == http.StatusConflict {
		switch apiErr.Reason {
		case "SNAPSHOT_UNAVAILABLE":
			return ports.ErrorKindSnapshotNotProcessed
		case "NO_QUALIFYING_SNAPSHOT":
			if apiErr.LatestSnapshotID != "" {
				return ports.ErrorKindSnapshotNotProcessed
			}
			return ports.ErrorKindNoSnapshots
		}
	}
	// FailedSnapshotAccessException, ProcessingCanceledException and
	// ProcessingTimedOutException are 400s with no reason code, so the fixed
	// texts ForwardHandlerExceptionResolver writes are all there is to match.
	if status == http.StatusBadRequest &&
		(strings.Contains(detail, "snapshot you are attempting to access failed to process") ||
			strings.Contains(detail, "processing was canceled for snapshot") ||
			strings.Contains(detail, "processing timed out for snapshot")) {
		return ports.ErrorKindSnapshotProcessingFailed
	}
	// SnapshotProcessingInterceptor.handleAdvancedReachability answers a snapshot that has not finished advanced reachability (the
	// stage interesting-path searches need) with a 400 and 'Advanced reachability <STATE> for snapshot <id>.' on any route.
	if status == http.StatusBadRequest && strings.Contains(detail, "advanced reachability") && strings.Contains(detail, "for snapshot") {
		if strings.Contains(detail, "canceled") || strings.Contains(detail, "timed_out") || strings.Contains(detail, "timed out") || strings.Contains(detail, "failed") {
			return ports.ErrorKindSnapshotProcessingFailed
		}
		return ports.ErrorKindSnapshotNotProcessed
	}
	if strings.HasPrefix(path, "/api/snapshots/") &&
		(status == http.StatusBadRequest || status == http.StatusConflict || status == http.StatusTooEarly) &&
		(status == http.StatusConflict && requestHasNoQuery(apiErr.Response) ||
			containsWords(detail, "snapshot", "not", "processed") ||
			containsWords(detail, "snapshot", "still", "processing") ||
			strings.Contains(detail, "snapshot processing") ||
			strings.Contains(detail, "snapshot unprocessed")) {
		return ports.ErrorKindSnapshotNotProcessed
	}
	// Before the network rules below: "No endpoint GET /api/networks/9/x." on a network path would otherwise read as a
	// missing network when the route is simply not served here.
	if status == http.StatusNotFound && apiErr.Reason == "" && isNoEndpointMessage(apiErr.Message, method) {
		return ports.ErrorKindEndpointNotServed
	}
	// The words are matched in the message, not the whole body: the body carries the appserver's apiUrl, and
	// "/api/networks/12/aws-cap-credentials/X" supplies "network" to any "X not found", so a missing child of a
	// network read as a missing network. The body is the fallback only when there is no message to read.
	notFoundDetail := detail
	if apiErr.Message != "" {
		notFoundDetail = normalizedErrorDetail(apiErr.Code, apiErr.Reason, apiErr.Message)
	}
	// ScheduledDiscoveryController words a missing schedule as 'Network "5" with scheduled discovery "7" not found'.
	missingSchedule := strings.Contains(notFoundDetail, "with scheduled discovery")
	// CustomCommandGroupRepo words a missing group 'Custom command group "x" not found': a group named like "network-checks"
	// would otherwise supply the word "network" to the rule below.
	missingSchedule = missingSchedule || strings.HasPrefix(notFoundDetail, "custom command group")
	if status == http.StatusNotFound && !missingSchedule &&
		(containsWords(notFoundDetail, "network", "not", "found") ||
			containsWords(notFoundDetail, "network", "does", "not", "exist") ||
			isNetworkResourcePath(path)) {
		return ports.ErrorKindNetworkNotFound
	}
	// TrustedCertificateTaskService.applyCertificates is the only handler on
	// this route that throws ConflictException, and only for action=apply.
	if status == http.StatusBadRequest &&
		strings.Contains(detail, "no enum constant") && strings.Contains(detail, "orgproperty") {
		return ports.ErrorKindUnknownOrgProperty
	}
	if status == http.StatusBadRequest &&
		strings.Contains(detail, "no enum constant") && strings.Contains(detail, "networkproperty") {
		return ports.ErrorKindUnknownNetworkProperty
	}
	// Requests.validate in NetworkEndpointService.deleteProfile: "Profile is still used in %s network(s): %s".
	if method == http.MethodDelete && status == http.StatusBadRequest && strings.HasPrefix(path, "/api/endpoint-profiles/") &&
		strings.Contains(detail, "profile is still used in") {
		return ports.ErrorKindEndpointProfileInUse
	}
	if method == http.MethodPost && path == "/api/trusted-certificates" &&
		status == http.StatusConflict && requestHasQueryValue(apiErr.Response, "action", "apply") {
		return ports.ErrorKindTrustedCertificateApplyInProgress
	}
	return ports.ErrorKindUnknown
}

// featureGateMessage is the whole message AccessEnforcer's property checks
// produce (PermissionsErrorMessages: "%s is %s for your organization" and
// "... for your deployment") -- a 403 with no reason code, so this fixed
// template is the only stable signal. It is anchored so only that exact
// sentence classifies, never a message that merely contains it.
// unrecognizedDevicesMessage is LayoutService's Requests.validate message with the device list Java prints, "[r1, r2]".
var unrecognizedDevicesMessage = regexp.MustCompile(`^Unrecognized devices cannot be moved: \[(.*)\]$`)

// checkFieldNotAllowedMessage is the whole message of Forward's refusal of a name, note, tags or perfMonitoringEnabled a check type does not
// take, or of a name it requires (NewNetworkCheck's constructor): "'name' not allowed for non-parameterized NQE checks", "'name' required
// for parameterized NQE checks", "'note' not allowed for NQE checks", "'tags' not allowed for Predefined checks" (the type is
// CheckType.toString(), its API spelling), "'perfMonitoringEnabled' not allowed for Isolation checks". Anchored.
var checkFieldNotAllowedMessage = regexp.MustCompile(`^'(name|note|tags|perfMonitoringEnabled)' (not allowed|required) for ((non-)?parameterized NQE|[A-Za-z]+) checks$`)

// orgNotAccessibleMessage is the whole message of Forward's refusal for an organization the user may not access. Anchored.
var orgNotAccessibleMessage = regexp.MustCompile(`^Org \S+ is not accessible to user \S+$`)

// serviceNowAsleepMessage is the whole message of Forward's refusal for a ServiceNow instance that answered its wake-up page. Anchored, so a
// message that merely contains the words does not classify.
var serviceNowAsleepMessage = regexp.MustCompile(`(?i)^ServiceNow instance is (hibernating|suspended)\.?$`)

// permissionMessage is the whole message of Forward's three operation denials. Anchored, so only those sentences classify.
var permissionMessage = regexp.MustCompile(`^(Missing permission|Unlicensed operation|No permission for network operation):? ((?:[A-Za-z]+Operation\.)?[A-Z][A-Z0-9_]*)$`)

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

// isNoEndpointMessage reports Spring's NoHandlerFoundException text as Forward writes it ("No endpoint " + method + " " +
// URL + "."), for the request's own method. Checked against spring-webmvc 7.0.9's class constants.
func isNoEndpointMessage(message, method string) bool {
	message = strings.TrimSpace(message)
	if method == "" || !strings.HasSuffix(message, ".") {
		return false
	}
	return strings.HasPrefix(strings.ToUpper(message), "NO ENDPOINT "+strings.ToUpper(method)+" ")
}

// isGone reports whether a failed delete means the thing is already absent, so the delete may count as done. A 404 that
// means Forward does not serve the route at all (ErrEndpointNotServed) does not qualify: nothing was deleted.

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
