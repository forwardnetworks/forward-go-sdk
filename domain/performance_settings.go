package domain

import (
	"encoding/json"
	"errors"
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
