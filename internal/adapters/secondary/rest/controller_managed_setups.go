package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// ControllerManagedSetupsService manages SD-WAN controller-managed setups:
// the record that tells Forward a controller (vManage, vBond, vSmart) owns a
// set of devices, rather than each device being collected classically.
//
// Published in Forward's OpenAPI (controller-managed-setups.yaml) but tagged Experimental: the contract may change without
// deprecation.
type ControllerManagedSetups service

func (s *ControllerManagedSetups) base(networkID string) (string, error) {
	return s.client.networkSubPath(networkID, "controller-managed-setups")
}

// List returns the network's controller-managed setups.
//
// Forward wraps them as {"setups": [...]}. A bare array and the other list
// envelopes are accepted too, because a reader that knows one shape does not
// fail on another -- it returns EMPTY, and an empty list here means "no setup
// exists", which sends the caller straight into a create that then collides
// with the setup that was sitting there ("Controller-managed setup named
// 'sdwan' already exists in network", measured on cs-lab network 3150).
//
// A body that is NEITHER an array nor an object with a known key is an
// ERROR, not an empty list -- an HTML login page or {"error":...} must not
// read as "this network has no setups".
func (s *ControllerManagedSetups) List(ctx context.Context, networkID string) ([]ports.ControllerManagedSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.list(ctx, networkID, "", "ControllerManagedSetups.List")
}

// ListWithTestResults is List with ?with=testResult: each controller and guest carries its last connectivity test
// (DeviceTestResult), which is where the outcome of Test shows up. The same read is available per device on classic devices
// through ClassicDevices.ListTestStatuses.
func (s *ControllerManagedSetups) ListWithTestResults(ctx context.Context, networkID string) ([]ports.ControllerManagedSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.list(ctx, networkID, "?with=testResult", "ControllerManagedSetups.ListWithTestResults")
}

func (s *ControllerManagedSetups) list(ctx context.Context, networkID, query, operation string) ([]ports.ControllerManagedSetup, *ports.Response, error) {
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	// AllowSingle is deliberately OFF. This list drives a delete-and-recreate
	// reconciliation, so a misread is not a cosmetic problem: with a single
	// object accepted, an ERROR body like {"error":"forbidden"} would decode
	// into one setup with an empty name, match nothing, and let the drifted
	// setup survive exactly as if the list had been empty. An object carrying
	// none of the known keys must therefore be an ERROR.
	result := listResponse[ports.ControllerManagedSetup]{
		Keys: []string{"setups", "data", "items", "results"},
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+query, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Create adds a controller-managed setup.
//
// Names are unique across every collection source in the network, so a controller or guest that a classic device
// already holds under the same name is refused with a 400; CreateMigratingClassic is the one atomic way to take such
// devices over.
func (s *ControllerManagedSetups) Create(ctx context.Context, networkID string, input ports.NewControllerManagedSetup) (*ports.ControllerManagedSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.create(ctx, networkID, input, "", "ControllerManagedSetups.Create")
}

// CreateMigratingClassic adds a controller-managed setup and, in the same transaction, DELETES every classic device
// whose name matches one of the setup's controllers or managed devices (POST ...controller-managed-setups?action=migrate,
// addControllerManagedSetupWithMigration, published as Experimental). It is how the UI moves devices that were added
// as classic devices into a setup: if the setup is refused, nothing is deleted; if it is accepted, the classic devices are
// gone, with their per-device settings, their connectivity test results and their device tags. Nothing is copied onto the new
// entry, so state in the request every credential (HTTPCredentialID for an API-collected controller such as a Versa Director),
// jump server, port and collection option (note, BGP, SNMP, pagination, prompts, thresholds: all fields of ControllerDevice and
// ManagedDevice) you want kept, or Forward falls back to auto-association and its defaults. Test results and tags cannot be carried.
// Only the setup's controllers and managed devices are matched, not the setup's own name (a classic device called that is
// still a 400). A name held by a
// non-classic source (another setup, an endpoint) is still a 400 and rolls the whole thing back. Names that match no
// classic device are simply added. Read the classic devices you are about to replace first.
func (s *ControllerManagedSetups) CreateMigratingClassic(ctx context.Context, networkID string, input ports.NewControllerManagedSetup) (*ports.ControllerManagedSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.create(ctx, networkID, input, "?action=migrate", "ControllerManagedSetups.CreateMigratingClassic")
}

func (s *ControllerManagedSetups) create(ctx context.Context, networkID string, input ports.NewControllerManagedSetup, query, operation string) (*ports.ControllerManagedSetup, *ports.Response, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, nil, errors.New("forward: controller-managed setup name is required")
	}
	if len(input.Controllers) == 0 {
		return nil, nil, errors.New("forward: a controller-managed setup requires at least one controller")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	input.Controllers = withoutTestResults(input.Controllers, func(d *ports.ControllerDevice) { d.TestResult = nil })
	input.ManagedDevices = withoutTestResults(input.ManagedDevices, func(d *ports.ManagedDevice) { d.TestResult = nil })
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+query, input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	out := new(ports.ControllerManagedSetup)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// Test queues a connectivity test of the selected controllers and managed devices (POST
// ...controller-managed-setups?action=test, testControllerManagedSetups, published as Experimental; the UI's "Test" button). Forward answers
// 204 as soon as the tests are queued, not when they finish: read each device's testResult afterwards (ListWithTestResults, or
// ClassicDevices.ListTestStatuses for a device still held as a classic one). SetupNames are matched case-sensitively against the stored (lowercase) names, so Test folds
// them to lowercase; a name that matches no setup tests nothing and still answers 204. collectorID, when not empty, runs them through that collector instead of the setup's own.
func (s *ControllerManagedSetups) Test(ctx context.Context, networkID, collectorID string, selection ports.ControllerAndManagedDeviceSelection) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if (len(selection.Names) == 0) == (len(selection.SetupNames) == 0) {
		return nil, errors.New("forward: exactly one of names or setupNames must be given")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	if len(selection.SetupNames) != 0 {
		folded := make([]string, len(selection.SetupNames))
		for i, name := range selection.SetupNames {
			folded[i] = strings.ToLower(strings.TrimSpace(name))
		}
		selection.SetupNames = folded
	}
	query := url.Values{"action": []string{"test"}}
	if collectorID = strings.TrimSpace(collectorID); collectorID != "" {
		query.Set("collectorId", collectorID)
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?"+query.Encode(), selection)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "ControllerManagedSetups.Test")
	return s.client.Do(req, nil)
}

// Delete removes a setup by name. A 404 is success: the desired state, that
// the setup is absent, already holds.
func (s *ControllerManagedSetups) Delete(ctx context.Context, networkID, setupName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	setupName = strings.TrimSpace(setupName)
	if setupName == "" {
		return nil, errors.New("forward: controller-managed setup name is required")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	// Forward stores setup names lowercased and a miss answers 404, which this method reads as "already gone", so a name sent in
	// another case would report success and delete nothing.
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path+"/"+url.PathEscape(strings.ToLower(setupName)), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "ControllerManagedSetups.Delete")
	resp, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
		return resp, nil
	}
	return resp, err
}

// Patch applies a partial update to an existing setup and returns it as stored.
//
// This is the idempotent way to declare a setup's guests. Forward compares the
// patched setup against the stored one and writes only if they differ
// (ControllerManagedSetupController.patchSetup: `if (!updated.equals(existing))`),
// so re-sending the same guest list is a no-op on the server -- it does not
// touch the controllers and does not churn the connectivity test result the way
// a delete-and-recreate would.
//
// NAME COLLISIONS ARE THE CALLER'S PROBLEM. Forward validates a device name as
// unique across ALL of a network's collection sources, so patching in a guest
// whose name is still held by a classic device fails with a 400. The classic
// record has to go first. Forward's own ?action=migrate create does exactly
// that (addSetupMigratingClassicDevices deletes the overlapping classic devices
// inside the same transaction), but there is no migrating form of PATCH.
//
// ManagedDevices and Controllers replace the stored list, so send whole entries (a list read back and re-sent round-trips
// every property the entries model, which is all of them except the guest's hidden concurrency and attribution). To change one
// controller or guest without restating the rest, use PatchController or PatchManagedDevice. A setup rename is not modelled.
func (s *ControllerManagedSetups) Patch(
	ctx context.Context,
	networkID, setupName string,
	patch ports.ControllerManagedSetupPatch,
) (*ports.ControllerManagedSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	setupName = strings.TrimSpace(setupName)
	if setupName == "" {
		return nil, nil, errors.New("forward: controller-managed setup name is required")
	}
	if patch.ClearCollector && patch.CollectorID != "" {
		return nil, nil, errors.New("forward: controller-managed setup patch both sets and clears the collector")
	}
	if patch.Controllers != nil && len(*patch.Controllers) == 0 {
		return nil, nil, errors.New("forward: a setup must keep at least one controller (Forward answers 400 to an empty list)")
	}
	base, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	if patch.Controllers != nil {
		cleaned := withoutTestResults(*patch.Controllers, func(d *ports.ControllerDevice) { d.TestResult = nil })
		patch.Controllers = &cleaned
	}
	if patch.ManagedDevices != nil {
		cleaned := withoutTestResults(*patch.ManagedDevices, func(d *ports.ManagedDevice) { d.TestResult = nil })
		patch.ManagedDevices = &cleaned
	}
	req, err := s.client.newJSONRequest(
		ctx,
		http.MethodPatch,
		base+"/"+url.PathEscape(strings.ToLower(setupName)),
		patch,
	)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ControllerManagedSetups.Patch")
	out := new(ports.ControllerManagedSetup)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// withoutTestResults copies entries with their testResult cleared. testResult is output-only (Forward's input record wants a full
// connectivity result, with a device name, which DeviceTestResult does not carry), so an entry read with ListWithTestResults and sent
// back must not carry it. The copy leaves the caller's slice, and its encoding elsewhere, alone.
func withoutTestResults[T any](in []T, clear func(*T)) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	copy(out, in)
	for i := range out {
		clear(&out[i])
	}
	return out
}

// PatchController changes one controller of a setup in place and returns it as stored. The patch is the same
// property set as ClassicDevicePatch (name, type, host, port, the CLI, HTTP, SNMP and jump-server ids, collection options,
// thresholds, Clear for explicit nulls), except that CollectorID is refused: a setup's collector is set on the setup,
// with Patch. A rename is allowed, done in place (the test result moves with it), follows Forward's naming rules, and is not
// idempotent: the old name answers 404 afterwards, so do not retry it with WithIdempotent. PATCH
// /api/networks/{id}/controller-managed-setups/{setup}/controllers/{name} (ControllerManagedSetupController.
// patchControllerDevice, ControllerDevicePatch; MANAGE_COLLECTION_SOURCES). ndoName is not in the patch; set it through
// Patch with Controllers.
func (s *ControllerManagedSetups) PatchController(ctx context.Context, networkID, setupName, controllerName string, patch ports.ClassicDevicePatch) (*ports.ControllerDevice, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.ControllerDevice)
	resp, err := s.patchMember(ctx, networkID, setupName, "controllers", controllerName, patch, "ControllerManagedSetups.PatchController", out)
	return out, resp, err
}

// PatchManagedDevice changes one guest device of a setup in place and returns it as stored; the patch is the same as for
// PatchController. Unlike Patch with ManagedDevices, it touches only the properties the patch states, so nothing the SDK
// does not model is dropped. PATCH /api/networks/{id}/controller-managed-setups/{setup}/managed-devices/{name}
// (ControllerManagedSetupController.patchManagedDevice, ManagedDevicePatch; MANAGE_COLLECTION_SOURCES).
func (s *ControllerManagedSetups) PatchManagedDevice(ctx context.Context, networkID, setupName, deviceName string, patch ports.ClassicDevicePatch) (*ports.ManagedDevice, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.ManagedDevice)
	resp, err := s.patchMember(ctx, networkID, setupName, "managed-devices", deviceName, patch, "ControllerManagedSetups.PatchManagedDevice", out)
	return out, resp, err
}

func (s *ControllerManagedSetups) patchMember(ctx context.Context, networkID, setupName, kind, memberName string, patch ports.ClassicDevicePatch, operation string, out any) (*ports.Response, error) {
	setupName, memberName = strings.TrimSpace(setupName), strings.TrimSpace(memberName)
	if setupName == "" || memberName == "" {
		return nil, errors.New("forward: controller-managed setup name and device name are required")
	}
	if patch.CollectorID != nil {
		return nil, errors.New("forward: a controller or managed device has no collector of its own; set it on the setup with Patch")
	}
	for _, name := range patch.Clear {
		if name == "collectorId" {
			return nil, errors.New("forward: a controller or managed device has no collector of its own; set it on the setup with Patch")
		}
	}
	base, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, base+"/"+url.PathEscape(strings.ToLower(setupName))+"/"+kind+"/"+url.PathEscape(memberName), patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	return s.client.Do(req, out)
}

// DeleteController removes one controller from a setup without restating the others (DELETE
// /api/networks/{id}/controller-managed-setups/{setup}/controllers/{name}, ControllerManagedSetupController.deleteControllerDevice,
// DELETE_COLLECTION_SOURCES, 204; mapped at both pinned builds). Removing the setup's LAST controller is a 400 (the setup would be
// left with none; ControllerManagedSetupService.deleteControllerDevice maps IllegalArgumentException to BadRequestException), so
// delete the whole setup with Delete instead. A 404 is success (the member, or its setup, is already absent), so a typo in the setup
// name also reads as success. Preview.
func (s *ControllerManagedSetups) DeleteController(ctx context.Context, networkID, setupName, controllerName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.deleteMember(ctx, networkID, setupName, "controllers", controllerName, "ControllerManagedSetups.DeleteController")
}

// DeleteManagedDevice removes one guest device from a setup without restating the others (DELETE
// .../controller-managed-setups/{setup}/managed-devices/{name}, deleteManagedDevice, DELETE_COLLECTION_SOURCES, 204). Removing the
// last managed device is a 400, as for DeleteController; a 404 is success. Preview.
func (s *ControllerManagedSetups) DeleteManagedDevice(ctx context.Context, networkID, setupName, deviceName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.deleteMember(ctx, networkID, setupName, "managed-devices", deviceName, "ControllerManagedSetups.DeleteManagedDevice")
}

func (s *ControllerManagedSetups) deleteMember(ctx context.Context, networkID, setupName, kind, memberName, operation string) (*ports.Response, error) {
	setupName, memberName = strings.TrimSpace(setupName), strings.TrimSpace(memberName)
	if setupName == "" || memberName == "" {
		return nil, errors.New("forward: controller-managed setup name and device name are required")
	}
	base, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, base+"/"+url.PathEscape(strings.ToLower(setupName))+"/"+kind+"/"+url.PathEscape(memberName), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	resp, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
		return resp, nil
	}
	return resp, err
}
