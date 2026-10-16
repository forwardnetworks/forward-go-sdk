package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// forwardDeviceName is Forward's device name rule (DeviceName: word characters, dots and dashes). A name of only dots is
// refused as well: the SDK resolves request paths against the base URL, which removes "." and ".." segments, so Start(".")
// would silently become the whole-network start route, which tests every classic device.
var forwardDeviceName = regexp.MustCompile(`^[\w.-]+$`)

func validTestDeviceName(name string) bool {
	return forwardDeviceName.MatchString(name) && strings.Trim(name, ".") != ""
}

// ConnectivityTestsService runs and reads the connectivity tests of a network's classic devices: the test a collector runs
// to reach a device, log in, discover its type and query it (the UI's "Test" button on the Classic Devices page). Controller-
// managed setups have their own, ControllerManagedSetups.Test.
//
// A test is a probe: the network's collector connects to the device and logs in with its credentials, so every call here
// touches the customer's devices, and repeated logins with a bad credential can lock an account. Start and BulkStart queue the
// tests and return at once (204); the outcome appears later in List and Get, replacing the previous result for the device.
// Forward keeps the last result until then, so compare a result's SavedAt/EndTime with the time you queued the test (use the
// response's Date header rather than your own clock to avoid skew).
//
// Limits and requirements, from ClientController and ConnectivityService:
//   - Device limit: this SDK caps one BulkStart call at MaxConnectivityTestDevices (100) names and refuses more before sending
//     (BulkStop has no cap: stopping is the brake); Forward itself has no per-call limit. Names must match Forward's device name
//     rule. A name that is not a classic device of the network (an unknown one, or an endpoint) is a 400 "Devices not found in
//     this network" for the whole call, queueing nothing, not a 404.
//   - Rate limit: none specific to these routes. Forward queues one task per device on the collector, which runs them within its
//     own concurrency setting (default 128 devices at once, shared with every network on that collector and with collections),
//     one test per device at a time, and within a jump server's session limit, so a large call is mostly a burst of logins. Only
//     the per-user API rate limiter applies (off unless the deployment enables API_RATE_LIMITING).
//   - Collector id: none is passed. Each device is tested through the collector it is pinned to (ClassicDevice.CollectorID), else
//     the network's default collector. A 400 comes back if the network has no default collector (even when every device is
//     pinned) or a collector needs an update; a pinned collector that no longer exists falls back to the default; an offline
//     collector is not refused, the tests just wait.
//   - Permission: Start, BulkStart, Stop and BulkStop need the network operation TEST_COLLECTION_SOURCES; List and Get need
//     VIEW_NETWORK_AND_SNAPSHOTS. A refusal is the typed 403 (ErrPermissionDenied).
//
// The whole-network start and stop routes (POST ...connectivityTests/start and /stop, no device named) are deliberately not
// wrapped: they test every classic device in the network. The endpoint variants (?type=endpoint) are not wrapped either.
// Preview: ClientController, unpublished.
type ConnectivityTestsService service

// MaxConnectivityTestDevices is the most devices one BulkStart or BulkStop call accepts. Forward has no limit of its own, and
// a call that names a large set queues a probe of each from one collector; a caller that really means more splits it into calls
// with time between them and watches List, rather than this SDK queueing thousands of logins in one request.
const MaxConnectivityTestDevices = 100

// ConnectivityTestResult is one device's stored connectivity test result: its name and what the test found, decoded as
// DeviceTestResult (error code and phase, discovered type, controllers, host IPs, the times). An empty Error with an EndTime is
// a pass; an empty Error and EndTime is a result with nothing in it.
type ConnectivityTestResult struct {
	DeviceName string `json:"deviceName"`
	DeviceTestResult
}

func (r *ConnectivityTestResult) UnmarshalJSON(data []byte) error {
	var head struct {
		DeviceName string `json:"deviceName"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	var result DeviceTestResult
	if err := result.UnmarshalJSON(data); err != nil {
		return err
	}
	*r = ConnectivityTestResult{DeviceName: head.DeviceName, DeviceTestResult: result}
	return nil
}

func (s *ConnectivityTestsService) base(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/connectivityTests", nil
}

func (s *ConnectivityTestsService) devicePath(networkID, deviceName, suffix string) (string, error) {
	if deviceName = strings.TrimSpace(deviceName); deviceName == "" {
		return "", errors.New("forward: device name is required")
	}
	if !validTestDeviceName(deviceName) {
		return "", fmt.Errorf("forward: %q is not a valid device name (letters, digits, underscore, dot and dash)", deviceName)
	}
	base, err := s.base(networkID)
	if err != nil {
		return "", err
	}
	return base + "/" + url.PathEscape(deviceName) + suffix, nil
}

func checkTestDevices(names []string, capped bool) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, name := range names {
		if name = strings.TrimSpace(name); name == "" || seen[name] {
			continue
		}
		if !validTestDeviceName(name) {
			return nil, fmt.Errorf("forward: %q is not a valid device name (letters, digits, underscore, dot and dash)", name)
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, errors.New("forward: at least one device name is required")
	}
	if capped && len(out) > MaxConnectivityTestDevices {
		return nil, fmt.Errorf("forward: %d devices exceeds the %d a single connectivity test call accepts", len(out), MaxConnectivityTestDevices)
	}
	return out, nil
}

// Start queues a connectivity test of one classic device (POST ...connectivityTests/{deviceName}/start; 204;
// TEST_COLLECTION_SOURCES).
func (s *ConnectivityTestsService) Start(ctx context.Context, networkID, deviceName string) (*Response, error) {
	path, err := s.devicePath(networkID, deviceName, "/start")
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "ConnectivityTests.Start")
	return s.client.Do(req, nil)
}

// BulkStart queues connectivity tests of the named classic devices (POST ...connectivityTests/bulkStart {"devices": [...]};
// 204). At most MaxConnectivityTestDevices names per call, duplicates folded. Forward refuses the whole call, queueing nothing,
// when any name is not a classic device of the network ("Devices not found in this network").
func (s *ConnectivityTestsService) BulkStart(ctx context.Context, networkID string, deviceNames []string) (*Response, error) {
	return s.bulk(ctx, networkID, deviceNames, true, "/bulkStart", "ConnectivityTests.BulkStart")
}

// Stop cancels a queued or running connectivity test of one device (POST ...connectivityTests/{deviceName}/stop; 204). A device
// with nothing queued is ignored. It also cancels a connectivity test a running collection queued for that device.
func (s *ConnectivityTestsService) Stop(ctx context.Context, networkID, deviceName string) (*Response, error) {
	path, err := s.devicePath(networkID, deviceName, "/stop")
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "ConnectivityTests.Stop")
	return s.client.Do(req, nil)
}

// BulkStop cancels queued or running connectivity tests of the named devices (POST ...connectivityTests/bulkStop; 204), with no
// cap. Names that are unknown or have nothing queued are ignored, and it also cancels connectivity tests a running collection
// queued for those devices.
func (s *ConnectivityTestsService) BulkStop(ctx context.Context, networkID string, deviceNames []string) (*Response, error) {
	return s.bulk(ctx, networkID, deviceNames, false, "/bulkStop", "ConnectivityTests.BulkStop")
}

func (s *ConnectivityTestsService) bulk(ctx context.Context, networkID string, deviceNames []string, capped bool, suffix, operation string) (*Response, error) {
	names, err := checkTestDevices(deviceNames, capped)
	if err != nil {
		return nil, err
	}
	base, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, base+suffix, map[string]any{"devices": names})
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	return s.client.Do(req, nil)
}

// List returns every stored connectivity test result of the network, one per tested device (GET ...connectivityTests;
// VIEW_NETWORK_AND_SNAPSHOTS). It can be large on a big network: for a few devices use Get.
func (s *ConnectivityTestsService) List(ctx context.Context, networkID string) ([]ConnectivityTestResult, *Response, error) {
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ConnectivityTests.List")
	var out []ConnectivityTestResult
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// Get returns one device's stored result (GET ...connectivityTests/{deviceName}); a device never tested is a 404
// ("Connectivity test result not found").
func (s *ConnectivityTestsService) Get(ctx context.Context, networkID, deviceName string) (*ConnectivityTestResult, *Response, error) {
	path, err := s.devicePath(networkID, deviceName, "")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ConnectivityTests.Get")
	out := new(ConnectivityTestResult)
	resp, err := s.client.doRequired(req, out)
	// The single-device route returns the bare result without deviceName (only List adds it).
	out.DeviceName = strings.TrimSpace(deviceName)
	return out, resp, err
}
