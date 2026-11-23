package rest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// Settings reads a network's SNMP performance-collection settings (GET /api/networks/{id}/performance/settings;
// PerformanceController.getPerformanceSettings; VIEW_PERFORMANCE_SETTINGS; Preview: not in the published spec).
func (s *Performance) Settings(ctx context.Context, networkID string) (*ports.PerformanceSettings, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := performanceNetworkPath(s.client, networkID, "/performance/settings")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Performance.Settings")
	out := new(ports.PerformanceSettings)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// PatchSettings changes a network's SNMP performance-collection settings and returns the result (PATCH
// /api/networks/{id}/performance/settings; PerformanceController.patchPerformanceSettings, JsonProp semantics:
// an absent property is left alone; EDIT_PERFORMANCE_SETTINGS; Preview: not in the published spec). Forward pushes the new
// settings to the collectors on the way, so enabling starts SNMP polling of every device that is eligible (a known vendor OS,
// collection on, SNMP performance collection on). Collectors.SetPerformanceCollection sends the same route but always writes
// enabled and cannot set the interval.
//
// Dependencies: while enabled is true the network needs at least one SNMP credential (Credentials.CreateSNMPCredential), and any
// patch that leaves it true is a 400 "At least one SNMP credential has to be configured in the network" without one. Forward stores
// the new settings BEFORE it checks, so that 400 does not undo the change. Polling also needs, per device, collection on and SNMP
// collection on (ClassicDevicePatch.EnableSNMPCollection) with a known vendor OS, and a collector (the device's CollectorID, else
// the network's default). Readiness reports what is missing.
func (s *Performance) PatchSettings(ctx context.Context, networkID string, patch ports.PerformanceSettingsPatch) (*ports.PerformanceSettings, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if patch.Enabled == nil && patch.IntervalMinutes == nil && !patch.ClearInterval {
		return nil, nil, errors.New("forward: a performance settings patch needs enabled, an interval or ClearInterval")
	}
	if patch.IntervalMinutes != nil && (*patch.IntervalMinutes < 1 || *patch.IntervalMinutes > 1440) {
		return nil, nil, fmt.Errorf("forward: performance interval %d minutes must be 1 to 1440", *patch.IntervalMinutes)
	}
	path, err := performanceNetworkPath(s.client, networkID, "/performance/settings")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Performance.PatchSettings")
	out := new(ports.PerformanceSettings)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// DefaultThresholds returns the default performance thresholds a device's own thresholds fall back to (GET
// /api/default-performance-thresholds; PerformanceController.getDefaultPerformanceThresholds; any signed-in user; Preview: not in
// the published spec). The per-device overrides are ClassicDevicePatch's High* and Medium* fields.
func (s *Performance) DefaultThresholds(ctx context.Context) (*ports.DefaultPerformanceThresholds, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/default-performance-thresholds", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Performance.DefaultThresholds")
	out := new(ports.DefaultPerformanceThresholds)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// DevicesWithData lists the devices that have performance data in a time window around a snapshot (GET
// /api/networks/{id}/performance/devices-with-data; VIEW_PERFORMANCE_DATA; Preview: not in the published spec). Only query's
// StartTime, EndTime and SnapshotID are used; with none, the window is the 24 hours before the snapshot's end. It is the quick
// way to see whether an upload or a collector's polling reached Forward.
func (s *Performance) DevicesWithData(ctx context.Context, networkID string, query ports.MetricQuery) ([]string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var out []string
	resp, err := s.metricGet(ctx, networkID, "/performance/devices-with-data", windowQuery(query), &out, "Performance.DevicesWithData")
	if err != nil {
		return nil, resp, err
	}
	if out == nil {
		out = []string{}
	}
	return out, resp, nil
}

// DeviceHealth returns one device's utilization level and health issues in the 24 hours before query.EndTime, as Forward sent it
// (GET /api/networks/{id}/devices/{device}/health; VIEW_PERFORMANCE_DATA; Preview: not in the published spec). Only query's EndTime
// and SnapshotID are used.
func (s *Performance) DeviceHealth(ctx context.Context, networkID, device string, query ports.MetricQuery) (ports.JSONDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	device = strings.TrimSpace(device)
	if device == "" {
		return nil, nil, errors.New("forward: device name is required")
	}
	var out ports.JSONDocument
	resp, err := s.metricGet(ctx, networkID, "/devices/"+url.PathEscape(device)+"/health", endQuery(query), &out, "Performance.DeviceHealth")
	return out, resp, err
}

// DevicesHealth is DeviceHealth for several devices, keyed by device name (POST /api/networks/{id}/device-health;
// VIEW_PERFORMANCE_DATA; Preview: not in the published spec).
func (s *Performance) DevicesHealth(ctx context.Context, networkID string, devices []string, query ports.MetricQuery) (ports.JSONDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	names := nonEmptyStrings(devices)
	if len(names) == 0 {
		return nil, nil, errors.New("forward: at least one device name is required")
	}
	var out ports.JSONDocument
	resp, err := s.metricPost(ctx, networkID, "/device-health", endQuery(query), names, &out, "Performance.DevicesHealth")
	return out, resp, err
}

// InterfaceHealth returns one interface's utilization and problems in the 24 hours before query.EndTime, as Forward sent it (GET
// /api/networks/{id}/devices/{device}/interfaces/{interface}/health; VIEW_PERFORMANCE_DATA; Preview: not in the published spec).
// A "/" in the interface name is percent-encoded.
func (s *Performance) InterfaceHealth(ctx context.Context, networkID, device, interfaceName string, query ports.MetricQuery) (ports.JSONDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	device, interfaceName = strings.TrimSpace(device), strings.TrimSpace(interfaceName)
	if device == "" || interfaceName == "" {
		return nil, nil, errors.New("forward: device and interface names are required")
	}
	var out ports.JSONDocument
	suffix := "/devices/" + url.PathEscape(device) + "/interfaces/" + url.PathEscape(interfaceName) + "/health"
	resp, err := s.metricGet(ctx, networkID, suffix, endQuery(query), &out, "Performance.InterfaceHealth")
	return out, resp, err
}

// windowQuery keeps the time window and snapshot of a MetricQuery and drops the fields that belong to the other metric routes, so
// they cannot reach a route that does not read them.
func windowQuery(q ports.MetricQuery) ports.MetricQuery {
	return ports.MetricQuery{StartTime: q.StartTime, EndTime: q.EndTime, SnapshotID: q.SnapshotID}
}

func endQuery(q ports.MetricQuery) ports.MetricQuery {
	return ports.MetricQuery{EndTime: q.EndTime, SnapshotID: q.SnapshotID}
}

// CollectorDevices fetches the devices a collector should poll for performance data, as Forward sends them: the serialized
// PerfDevicesPB protobuf (GET /api/networks/{id}/performance/devices; PerformanceController.getDevicesForPerfMonitoring,
// @RequiresCollector; Preview: not in the published spec). The collector identity is the one Collectors.Register returned, and it
// is sent for this request only. A collector is assigned to devices, not to a network: the answer holds the network's devices that
// name this collector, plus the devices that name none when this collector is the network's default (Collectors.Attachment),
// limited to devices with a known vendor OS, collection on and SNMP performance collection on. The bytes are empty when there are
// none. The SDK has no protobuf dependency, so decoding them is the caller's.
func (s *Performance) CollectorDevices(ctx context.Context, networkID string, identity ports.CollectorIdentity) ([]byte, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.collectorRequest(ctx, http.MethodGet, networkID, identity, "/performance/devices")
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req = markOperation(req, "Performance.CollectorDevices")
	var buf bytes.Buffer
	resp, err := s.client.Do(req, &buf)
	if err != nil {
		return nil, resp, err
	}
	return buf.Bytes(), resp, nil
}

// collectorRequest builds a request on a network's performance routes that is authenticated with a collector identity.
func (s *Performance) collectorRequest(ctx context.Context, method, networkID string, identity ports.CollectorIdentity, suffix string) (*http.Request, error) {
	collectorUsername := strings.TrimSpace(identity.Username)
	if collectorUsername == "" || identity.AuthorizationKey == "" {
		return nil, errors.New("forward: collector username and password are required")
	}
	path, err := performanceNetworkPath(s.client, networkID, suffix)
	if err != nil {
		return nil, err
	}
	auth := &requestAuth{mode: ports.AuthModeCollector, username: collectorUsername, password: identity.AuthorizationKey}
	return s.client.newScopedRequest(ctx, method, path, nil, pathScopeAPI, auth)
}

// RawSNMPData has a collector walk a device over SNMP with a stored credential and returns the raw readings as Forward sent them
// (GET /api/networks/{id}/performance/raw-data; PerformanceController.getRawSnmpData, answered when the collector is done;
// MANAGE_COLLECTION_SOURCES; Preview: not in the published spec). It is the check to run before enabling performance collection
// for a device: it shows whether the credential, port and address work and which OIDs answer. The walk runs from the network's
// DEFAULT collector (Collectors.Attachment), not from the collector the device is pinned to, and Forward answers 400 when the network
// has no online, supported default collector or when Device is not a classic device with a known vendor OS.
func (s *Performance) RawSNMPData(ctx context.Context, networkID string, request ports.RawSNMPRequest) (ports.JSONDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	device, credentialID := strings.TrimSpace(request.Device), strings.TrimSpace(request.CredentialID)
	if device == "" || credentialID == "" {
		return nil, nil, errors.New("forward: device and SNMP credential ID are required")
	}
	oids := nonEmptyStrings(request.ParentOIDs)
	if len(oids) == 0 && !request.IncludeDevicePerfData {
		return nil, nil, errors.New("forward: give at least one parent OID or set IncludeDevicePerfData")
	}
	switch request.InterfaceDataProvider {
	case "", "ASYNC_TREE_WALK", "BULK_READ_TABLE":
	default:
		return nil, nil, fmt.Errorf("forward: interface data provider %q must be ASYNC_TREE_WALK or BULK_READ_TABLE", request.InterfaceDataProvider)
	}
	path, err := performanceNetworkPath(s.client, networkID, "/performance/raw-data")
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"deviceName": {device}, "credentialId": {credentialID}}
	for _, oid := range oids {
		query.Add("parentOids", oid)
	}
	if request.IncludeDevicePerfData {
		query.Set("includeDevicePerfData", "true")
	}
	if request.InterfaceDataProvider != "" {
		query.Set("interfaceDataProviderType", request.InterfaceDataProvider)
	}
	if v := strings.TrimSpace(request.DeviceAddressOverride); v != "" {
		query.Set("deviceAddressOverride", v)
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Performance.RawSNMPData")
	var out ports.JSONDocument
	resp, err := s.client.doWithTimeout(req, &out, false, nil, ports.RawSNMPDataTimeout)
	return out, resp, err
}
