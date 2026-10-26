package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultPerformanceIntervalMinutes is the polling interval Forward uses when a network has not set one
// (PerformanceSettings.getIntervalMinutesOrDefault).
const DefaultPerformanceIntervalMinutes = 5

// RawSNMPDataTimeout bounds RawSNMPData. Forward waits for the collector's walk for up to ten minutes (a timeout of its own, plus
// a margin), so a shorter client limit would give up while the collector is still working and, with a RetryPolicy set, queue a
// second walk.
const RawSNMPDataTimeout = 10*time.Minute + 30*time.Second

// PerformanceSettings is a network's SNMP performance-collection settings (Forward's PerformanceSettings).
type PerformanceSettings struct {
	// Enabled says whether the collectors poll the network's devices over SNMP for CPU, memory and interface counters.
	Enabled bool `json:"enabled"`
	// IntervalMinutes is the polling interval, nil when the network has not set one (then Interval gives the default).
	IntervalMinutes *int `json:"intervalMinutes,omitempty"`
}

// Interval returns the polling interval in minutes, DefaultPerformanceIntervalMinutes when none is set.
func (p PerformanceSettings) Interval() int {
	if p.IntervalMinutes == nil {
		return DefaultPerformanceIntervalMinutes
	}
	return *p.IntervalMinutes
}

// PerformanceSettingsPatch changes some of a network's performance settings. A nil field is left as it is.
type PerformanceSettingsPatch struct {
	Enabled *bool
	// IntervalMinutes is 1 to 1440. Forward answers 400 outside that range.
	IntervalMinutes *int
	// ClearInterval resets the interval to Forward's default by sending an explicit null.
	ClearInterval bool
}

// MarshalJSON sends the set properties, and a JSON null for the interval when ClearInterval is set.
func (p PerformanceSettingsPatch) MarshalJSON() ([]byte, error) {
	body := map[string]any{}
	if p.Enabled != nil {
		body["enabled"] = *p.Enabled
	}
	switch {
	case p.IntervalMinutes != nil && p.ClearInterval:
		return nil, errors.New("forward: performance settings patch both sets and clears the interval")
	case p.IntervalMinutes != nil:
		body["intervalMinutes"] = *p.IntervalMinutes
	case p.ClearInterval:
		body["intervalMinutes"] = nil
	}
	return json.Marshal(body)
}

// Settings reads a network's SNMP performance-collection settings (GET /api/networks/{id}/performance/settings;
// PerformanceController.getPerformanceSettings; VIEW_PERFORMANCE_SETTINGS; Preview: not in the published spec).
func (s *PerformanceService) Settings(ctx context.Context, networkID string) (*PerformanceSettings, *Response, error) {
	path, err := performanceNetworkPath(s.client, networkID, "/performance/settings")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Performance.Settings")
	out := new(PerformanceSettings)
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
func (s *PerformanceService) PatchSettings(ctx context.Context, networkID string, patch PerformanceSettingsPatch) (*PerformanceSettings, *Response, error) {
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
	out := new(PerformanceSettings)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// DefaultPerformanceThresholds are Forward's default health thresholds, as fractions of 1 (DefaultUtilizationThresholds).
// A nil field is one Forward did not state.
type DefaultPerformanceThresholds struct {
	MinimumHighThreshold    *float32 `json:"minimumHighThreshold,omitempty"`
	HighCPUUsage            *float32 `json:"highCpuUsage,omitempty"`
	HighMemoryUsage         *float32 `json:"highMemoryUsage,omitempty"`
	HighInputUtilization    *float32 `json:"highInputUtilization,omitempty"`
	HighOutputUtilization   *float32 `json:"highOutputUtilization,omitempty"`
	MinimumMediumThreshold  *float32 `json:"minimumMediumThreshold,omitempty"`
	MediumCPUUsage          *float32 `json:"mediumCpuUsage,omitempty"`
	MediumMemoryUsage       *float32 `json:"mediumMemoryUsage,omitempty"`
	MediumInputUtilization  *float32 `json:"mediumInputUtilization,omitempty"`
	MediumOutputUtilization *float32 `json:"mediumOutputUtilization,omitempty"`
}

// DefaultThresholds returns the default performance thresholds a device's own thresholds fall back to (GET
// /api/default-performance-thresholds; PerformanceController.getDefaultPerformanceThresholds; any signed-in user; Preview: not in
// the published spec). The per-device overrides are ClassicDevicePatch's High* and Medium* fields.
func (s *PerformanceService) DefaultThresholds(ctx context.Context) (*DefaultPerformanceThresholds, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/default-performance-thresholds", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Performance.DefaultThresholds")
	out := new(DefaultPerformanceThresholds)
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
func (s *PerformanceService) DevicesWithData(ctx context.Context, networkID string, query MetricQuery) ([]string, *Response, error) {
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
func (s *PerformanceService) DeviceHealth(ctx context.Context, networkID, device string, query MetricQuery) (JSONDocument, *Response, error) {
	device = strings.TrimSpace(device)
	if device == "" {
		return nil, nil, errors.New("forward: device name is required")
	}
	var out JSONDocument
	resp, err := s.metricGet(ctx, networkID, "/devices/"+url.PathEscape(device)+"/health", endQuery(query), &out, "Performance.DeviceHealth")
	return out, resp, err
}

// DevicesHealth is DeviceHealth for several devices, keyed by device name (POST /api/networks/{id}/device-health;
// VIEW_PERFORMANCE_DATA; Preview: not in the published spec).
func (s *PerformanceService) DevicesHealth(ctx context.Context, networkID string, devices []string, query MetricQuery) (JSONDocument, *Response, error) {
	names := nonEmptyStrings(devices)
	if len(names) == 0 {
		return nil, nil, errors.New("forward: at least one device name is required")
	}
	var out JSONDocument
	resp, err := s.metricPost(ctx, networkID, "/device-health", endQuery(query), names, &out, "Performance.DevicesHealth")
	return out, resp, err
}

// InterfaceHealth returns one interface's utilization and problems in the 24 hours before query.EndTime, as Forward sent it (GET
// /api/networks/{id}/devices/{device}/interfaces/{interface}/health; VIEW_PERFORMANCE_DATA; Preview: not in the published spec).
// A "/" in the interface name is percent-encoded.
func (s *PerformanceService) InterfaceHealth(ctx context.Context, networkID, device, interfaceName string, query MetricQuery) (JSONDocument, *Response, error) {
	device, interfaceName = strings.TrimSpace(device), strings.TrimSpace(interfaceName)
	if device == "" || interfaceName == "" {
		return nil, nil, errors.New("forward: device and interface names are required")
	}
	var out JSONDocument
	suffix := "/devices/" + url.PathEscape(device) + "/interfaces/" + url.PathEscape(interfaceName) + "/health"
	resp, err := s.metricGet(ctx, networkID, suffix, endQuery(query), &out, "Performance.InterfaceHealth")
	return out, resp, err
}

// windowQuery keeps the time window and snapshot of a MetricQuery and drops the fields that belong to the other metric routes, so
// they cannot reach a route that does not read them.
func windowQuery(q MetricQuery) MetricQuery {
	return MetricQuery{StartTime: q.StartTime, EndTime: q.EndTime, SnapshotID: q.SnapshotID}
}

func endQuery(q MetricQuery) MetricQuery {
	return MetricQuery{EndTime: q.EndTime, SnapshotID: q.SnapshotID}
}

// CollectorDevices fetches the devices a collector should poll for performance data, as Forward sends them: the serialized
// PerfDevicesPB protobuf (GET /api/networks/{id}/performance/devices; PerformanceController.getDevicesForPerfMonitoring,
// @RequiresCollector; Preview: not in the published spec). The collector identity is the one Collectors.Register returned, and it
// is sent for this request only. A collector is assigned to devices, not to a network: the answer holds the network's devices that
// name this collector, plus the devices that name none when this collector is the network's default (Collectors.Attachment),
// limited to devices with a known vendor OS, collection on and SNMP performance collection on. The bytes are empty when there are
// none. The SDK has no protobuf dependency, so decoding them is the caller's.
func (s *PerformanceService) CollectorDevices(ctx context.Context, networkID string, identity CollectorIdentity) ([]byte, *Response, error) {
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
func (s *PerformanceService) collectorRequest(ctx context.Context, method, networkID string, identity CollectorIdentity, suffix string) (*http.Request, error) {
	collectorUsername := strings.TrimSpace(identity.Username)
	if collectorUsername == "" || identity.AuthorizationKey == "" {
		return nil, errors.New("forward: collector username and password are required")
	}
	path, err := performanceNetworkPath(s.client, networkID, suffix)
	if err != nil {
		return nil, err
	}
	auth := &requestAuth{mode: AuthModeCollector, username: collectorUsername, password: identity.AuthorizationKey}
	return s.client.newScopedRequest(ctx, method, path, nil, pathScopeAPI, auth)
}

// RawSNMPRequest asks a collector to walk a device over SNMP and return what it read.
type RawSNMPRequest struct {
	// Device is the device name, as Forward knows it.
	Device string
	// CredentialID is the SNMP credential to use (Credentials.ListSNMP).
	CredentialID string
	// ParentOIDs are the OID subtrees to walk. At least one is needed unless IncludeDevicePerfData is set.
	ParentOIDs []string
	// IncludeDevicePerfData also returns the CPU, memory and uptime readings the performance poll would take.
	IncludeDevicePerfData bool
	// InterfaceDataProvider is ASYNC_TREE_WALK (Forward's default) or BULK_READ_TABLE; empty leaves it to Forward.
	InterfaceDataProvider string
	// DeviceAddressOverride polls this IP address instead of the device's configured one.
	DeviceAddressOverride string
}

// RawSNMPData has a collector walk a device over SNMP with a stored credential and returns the raw readings as Forward sent them
// (GET /api/networks/{id}/performance/raw-data; PerformanceController.getRawSnmpData, answered when the collector is done;
// MANAGE_COLLECTION_SOURCES; Preview: not in the published spec). It is the check to run before enabling performance collection
// for a device: it shows whether the credential, port and address work and which OIDs answer. The walk runs from the network's
// DEFAULT collector (Collectors.Attachment), not from the collector the device is pinned to, and Forward answers 400 when the network
// has no online, supported default collector or when Device is not a classic device with a known vendor OS.
func (s *PerformanceService) RawSNMPData(ctx context.Context, networkID string, request RawSNMPRequest) (JSONDocument, *Response, error) {
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
	var out JSONDocument
	resp, err := s.client.doWithTimeout(req, &out, false, nil, RawSNMPDataTimeout)
	return out, resp, err
}

// PerformanceReadiness reports what Forward's own SNMP performance polling of a network needs and what is missing. It is a read of
// three existing routes (the network's performance settings, its SNMP credentials and its classic devices), not a Forward answer.
//
// Forward polls a device only when ALL of these hold: the network's performance setting is enabled (and the network has an SNMP
// credential, or enabling is refused); the device has collection on and SNMP collection on; the device's vendor OS is known (stated
// on the device or learned from a connectivity test); and a collector serves the device (its CollectorID, else the network's default).
// Readiness checks the network setting, the credential and the two device flags. It cannot see a vendor OS that Forward learned from
// a connectivity test (the device list carries only the Type the device states), or whether the collector that serves the device
// (its CollectorID, else the network's default) is online and supported.
//
// Uploading performance data as a collector (UploadWithIdentity) needs none of this. Eligible is a necessary condition for Forward
// to poll a device, not a sufficient one: a device in Eligible MAY be polled by Forward, if its vendor OS is known and a supported
// collector serves it, and is not polled at all while Enabled is false or the network has no SNMP credential (Forward stores the
// setting before it refuses to enable it without one). Do not skip an upload for a device only because it is in Eligible; confirm
// with DevicesWithData that Forward is already receiving its data.
type PerformanceReadiness struct {
	// Enabled is the network's performance setting; IntervalMinutes is its polling interval (the default when unset).
	Enabled         bool
	IntervalMinutes int
	// SNMPCredentials counts the network's SNMP credentials.
	SNMPCredentials int
	// Devices counts the network's classic devices.
	Devices int
	// Eligible names the devices that pass the device-level flags (collection on and SNMP collection on).
	Eligible []string
	// Excluded names the devices that do not, each with the reasons.
	Excluded []PerformanceExcludedDevice
	// Blockers says, in words, what stops Forward polling anything at all; empty means nothing found stops it.
	Blockers []string
}

// PerformanceExcludedDevice is a device Forward will not poll, and why.
type PerformanceExcludedDevice struct {
	Name    string
	Reasons []string
}

// Ready reports whether nothing found stops Forward polling at least one device.
func (r PerformanceReadiness) Ready() bool { return len(r.Blockers) == 0 }

// Readiness reads the network's performance settings, SNMP credentials and classic devices and reports what Forward's SNMP
// performance polling still needs (see PerformanceReadiness). It makes three requests and needs VIEW_PERFORMANCE_SETTINGS,
// VIEW_DEVICE_CREDENTIALS and permission to read the network's collection sources. Preview: composed from routes that are not in
// the published spec. A failure of any of the three is returned as it is, with no partial answer.
func (s *PerformanceService) Readiness(ctx context.Context, networkID string) (*PerformanceReadiness, *Response, error) {
	// Resolved once so all three reads use the same network, including the bound one of a ForNetwork client.
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	settings, resp, err := s.Settings(ctx, networkID)
	if err != nil {
		return nil, resp, err
	}
	credentials, resp, err := s.client.Credentials.ListSNMPCredentials(ctx, networkID)
	if err != nil {
		return nil, resp, err
	}
	devices, resp, err := s.client.ClassicDevices.List(ctx, networkID)
	if err != nil {
		return nil, resp, err
	}
	out := &PerformanceReadiness{
		Enabled:         settings.Enabled,
		IntervalMinutes: settings.Interval(),
		SNMPCredentials: len(credentials),
		Devices:         len(devices),
		Eligible:        []string{},
		Excluded:        []PerformanceExcludedDevice{},
		Blockers:        []string{},
	}
	for _, device := range devices {
		var reasons []string
		// Collection defaults to on when the read does not state it; SNMP collection defaults to off.
		if device.Collect != nil && !*device.Collect {
			reasons = append(reasons, "collection is off")
		}
		if device.EnableSNMPCollection == nil || !*device.EnableSNMPCollection {
			reasons = append(reasons, "SNMP collection is off")
		}
		if len(reasons) == 0 {
			out.Eligible = append(out.Eligible, device.Name)
		} else {
			out.Excluded = append(out.Excluded, PerformanceExcludedDevice{Name: device.Name, Reasons: reasons})
		}
	}
	if !out.Enabled {
		out.Blockers = append(out.Blockers, "performance collection is disabled for the network (PatchSettings)")
	}
	if out.SNMPCredentials == 0 {
		out.Blockers = append(out.Blockers, "the network has no SNMP credential (Credentials.CreateSNMPCredential); Forward refuses to enable collection without one")
	}
	if len(out.Eligible) == 0 {
		out.Blockers = append(out.Blockers, "no device has both collection and SNMP collection on (ClassicDevicePatch.EnableSNMPCollection)")
	}
	return out, resp, nil
}
