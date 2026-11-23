package domain

import (
	"encoding/json"
	"errors"
	"fmt"
)

// DiscoveryParams is what Start and a scheduled discovery run: SubnetScanParams, SeededDiscoveryParams, DirectDiscoveryParams
// (scheduled only) or CloudDiscoveryParams (Start only). Each marshals with the "type" property Forward's
// DeviceDiscoveryParams union dispatches on.
type DiscoveryParams interface {
	DiscoveryType() string
	Validate() error
	// isDiscoveryParams seals the interface: only the four parameter types here implement it, so DeviceDiscovery.Start sends only
	// a discovery type Forward defines.
	isDiscoveryParams()
}

// SubnetScanParams scans subnets and hosts (DeviceDiscoveryType SUBNET_SCAN). One of Subnets or Hosts is required. A subnet
// wider than /24 is refused unless ConnectionScanPorts is set (a connection-only scan, up to /8 for IPv4; /120 and /104 for
// IPv6), and each port must be 1 to 65535; Forward checks all of that and answers 400. Protocols is any of HTTP, HTTPS, SSH, TELNET, SNMP; empty means Forward's own set. The collector is not part of
// the body: pass it to Start.
type SubnetScanParams struct {
	Subnets             []string `json:"subnets,omitempty"`
	Hosts               []string `json:"hosts,omitempty"`
	SubnetsToSkip       []string `json:"subnetsToSkip,omitempty"`
	HostnamesToSkip     []string `json:"hostnamesToSkip,omitempty"`
	JumpServerIDs       []string `json:"jumpServerIds,omitempty"`
	TryDirectConnection bool     `json:"tryDirectConnection,omitempty"`
	Protocols           []string `json:"protocols,omitempty"`
	ConnectionScanPorts []int    `json:"connectionScanPorts,omitempty"`
}

func (SubnetScanParams) isDiscoveryParams() {}

func (SubnetScanParams) DiscoveryType() string { return "SUBNET_SCAN" }

func (p SubnetScanParams) Validate() error {
	if len(p.Subnets) == 0 && len(p.Hosts) == 0 {
		return errors.New("forward: a subnet scan needs subnets or hosts")
	}
	return nil
}

func (p SubnetScanParams) MarshalJSON() ([]byte, error) {
	type plain SubnetScanParams
	return marshalWithType(p.DiscoveryType(), plain(p))
}

// SeededDiscoveryParams discovers devices by following seed devices' neighbours (SEEDED). SeedDevices are IP addresses.
type SeededDiscoveryParams struct {
	SeedDevices      []string `json:"seedDevices"`
	SNMPCredentialID string   `json:"snmpCredentialId,omitempty"`
	JumpServerID     string   `json:"jumpServerId,omitempty"`
	Recursive        bool     `json:"recursive,omitempty"`
	AllowedSubnets   []string `json:"allowedSubnets,omitempty"`
	SubnetsToSkip    []string `json:"subnetsToSkip,omitempty"`
	HostsToSkip      []string `json:"hostsToSkip,omitempty"`
}

func (SeededDiscoveryParams) isDiscoveryParams() {}

func (SeededDiscoveryParams) DiscoveryType() string { return "SEEDED" }

func (p SeededDiscoveryParams) Validate() error {
	if len(p.SeedDevices) == 0 {
		return errors.New("forward: seeded discovery needs seed devices")
	}
	return nil
}

func (p SeededDiscoveryParams) MarshalJSON() ([]byte, error) {
	type plain SeededDiscoveryParams
	return marshalWithType(p.DiscoveryType(), plain(p))
}

// DirectDiscoveryParams is the DIRECT type (CDP/LLDP neighbours of the network's own devices). It carries nothing: Forward
// builds the device list itself from the network's classic devices (DeviceDiscoveryJobService.buildDirectParams), and the
// UI's DIRECT wizard sends exactly {"type":"DIRECT"}. Allowed for Start and for scheduled discovery.
type DirectDiscoveryParams struct{}

func (DirectDiscoveryParams) isDiscoveryParams() {}

func (DirectDiscoveryParams) DiscoveryType() string { return "DIRECT" }

func (DirectDiscoveryParams) Validate() error { return nil }

func (p DirectDiscoveryParams) MarshalJSON() ([]byte, error) {
	return marshalWithType(p.DiscoveryType(), struct{}{})
}

// CloudDiscoveryParams discovers devices from existing cloud setups (CLOUD); SetupIDs are the setup names. Start only:
// Forward does not schedule it.
type CloudDiscoveryParams struct {
	SetupIDs []string `json:"setupIds"`
}

func (CloudDiscoveryParams) isDiscoveryParams() {}

func (CloudDiscoveryParams) DiscoveryType() string { return "CLOUD" }

func (p CloudDiscoveryParams) Validate() error {
	if len(p.SetupIDs) == 0 {
		return errors.New("forward: cloud discovery needs setup ids")
	}
	return nil
}

func (p CloudDiscoveryParams) MarshalJSON() ([]byte, error) {
	type plain CloudDiscoveryParams
	return marshalWithType(p.DiscoveryType(), plain(p))
}

func marshalWithType(discoveryType string, params any) ([]byte, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	fields["type"], _ = json.Marshal(discoveryType)
	return json.Marshal(fields)
}

func decodeDiscoveryParams(raw json.RawMessage) (DiscoveryParams, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, err
	}
	var params DiscoveryParams
	var target any
	switch head.Type {
	case "SUBNET_SCAN":
		p := &SubnetScanParams{}
		params, target = p, p
	case "SEEDED":
		p := &SeededDiscoveryParams{}
		params, target = p, p
	case "CLOUD":
		p := &CloudDiscoveryParams{}
		params, target = p, p
	case "DIRECT":
		return DirectDiscoveryParams{}, nil
	default:
		return nil, fmt.Errorf("forward: unknown discovery type %q", head.Type)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return nil, err
	}
	switch p := params.(type) {
	case *SubnetScanParams:
		return *p, nil
	case *SeededDiscoveryParams:
		return *p, nil
	default:
		return *(params.(*CloudDiscoveryParams)), nil
	}
}

// DiscoveryJob is one discovery run (DeviceDiscoveryJobWithAttribution). EndedAt is empty while it runs. Error is "NONE"
// unless the whole job failed.
type DiscoveryJob struct {
	ID              string     `json:"id"`
	Type            string     `json:"type"`
	CollectorID     Identifier `json:"collectorId,omitempty"`
	StartedByUserID Identifier `json:"startedByUserId,omitempty"`
	// ScheduledDiscoveryID is the scheduled discovery that started the job, when one did.
	ScheduledDiscoveryID string          `json:"scheduledDiscoveryId,omitempty"`
	StartedBy            string          `json:"startedBy,omitempty"`
	StartedAt            string          `json:"startedAt,omitempty"`
	EndedAt              string          `json:"endedAt,omitempty"`
	InitialQueueSize     *int            `json:"initialQueueSize,omitempty"`
	Counts               DiscoveryCounts `json:"counts"`
	Error                string          `json:"error,omitempty"`
}

// DiscoveryCounts are a job's progress: still queued, found, and not reachable.
type DiscoveryCounts struct {
	Queued       int `json:"queued"`
	Discovered   int `json:"discovered"`
	Inaccessible int `json:"inaccessible"`
}

// DiscoveredDevice is a device a job found. Type is the connection type ("unknown" when it could not be inferred; then
// Neighbors may help). To add it as a classic device use ClassicDevices with CollectorID of the job in JobID (the job's
// collector is where Forward could reach it from). The cloud fields are set for CLOUD discovery only.
type DiscoveredDevice struct {
	Name             string   `json:"name"`
	Type             string   `json:"type,omitempty"`
	Host             string   `json:"host,omitempty"`
	Port             *int32   `json:"port,omitempty"`
	CLICredentialID  string   `json:"cliCredentialId,omitempty"`
	HTTPCredentialID string   `json:"httpCredentialId,omitempty"`
	SNMPCredentialID string   `json:"snmpCredentialId,omitempty"`
	JumpServerID     string   `json:"jumpServerId,omitempty"`
	Vendor           string   `json:"vendor,omitempty"`
	Neighbors        []string `json:"neighbors,omitempty"`
	// JobID is the job that most recently discovered it; DiscoveredAt when its collector uploaded the result.
	JobID        string `json:"jobId,omitempty"`
	DiscoveredAt string `json:"discoveredAt,omitempty"`

	IsReachable          *bool    `json:"isReachable,omitempty"`
	DiscoveredIPs        []string `json:"discoveredIps,omitempty"`
	CloudLocationID      string   `json:"cloudLocationId,omitempty"`
	CloudSetupID         string   `json:"cloudSetupId,omitempty"`
	CloudDiscoverySource string   `json:"cloudDiscoverySource,omitempty"`
}

// DiscoveredDevices is the jobs (always including the most recent) and devices ListDiscovered returns.
type DiscoveredDevices struct {
	Jobs    []DiscoveryJob     `json:"jobs"`
	Devices []DiscoveredDevice `json:"devices"`
}

// JobFor is the job that discovered d, or nil when it is not listed.
func (r *DiscoveredDevices) JobFor(d DiscoveredDevice) *DiscoveryJob {
	for i := range r.Jobs {
		if r.Jobs[i].ID == d.JobID {
			return &r.Jobs[i]
		}
	}
	return nil
}

// DiscoverySchedule is when a scheduled discovery runs. Set the weekly form (DaysOfTheWeek and Times, both "HH:mm") or the
// monthly form (DayOfTheWeek, WeekOfTheMonth 1 to 4, Time), never both; a nil *DiscoverySchedule in a definition means the
// discovery piggybacks on every collection. Days are Sun (0) to Sat (6). TimeZone is one of Forward's zone ids; empty uses
// the organization's.
type DiscoverySchedule struct {
	TimeZone       string   `json:"timeZone,omitempty"`
	DaysOfTheWeek  []int    `json:"daysOfTheWeek,omitempty"`
	Times          []string `json:"times,omitempty"`
	DayOfTheWeek   *int     `json:"dayOfTheWeek,omitempty"`
	WeekOfTheMonth *int     `json:"weekOfTheMonth,omitempty"`
	Time           string   `json:"time,omitempty"`
}

func (d DiscoverySchedule) Check() error {
	weekly := len(d.DaysOfTheWeek) != 0 || len(d.Times) != 0
	monthly := d.DayOfTheWeek != nil || d.WeekOfTheMonth != nil || d.Time != ""
	if weekly == monthly {
		return errors.New("forward: a discovery schedule is weekly (DaysOfTheWeek and Times) or monthly (DayOfTheWeek, WeekOfTheMonth, Time), not both or neither")
	}
	if weekly {
		if len(d.DaysOfTheWeek) == 0 || len(d.Times) == 0 {
			return errors.New("forward: a weekly discovery schedule needs DaysOfTheWeek and Times")
		}
		for _, day := range d.DaysOfTheWeek {
			if day < 0 || day > 6 {
				return fmt.Errorf("forward: day of the week %d must be 0 (Sun) to 6 (Sat)", day)
			}
		}
		for _, t := range d.Times {
			if !scheduleClock.MatchString(t) {
				return fmt.Errorf("forward: schedule time %q must be HH:mm", t)
			}
		}
		return nil
	}
	if d.DayOfTheWeek == nil || d.WeekOfTheMonth == nil || d.Time == "" {
		return errors.New("forward: a monthly discovery schedule needs DayOfTheWeek, WeekOfTheMonth and Time")
	}
	if *d.DayOfTheWeek < 0 || *d.DayOfTheWeek > 6 || *d.WeekOfTheMonth < 1 || *d.WeekOfTheMonth > 4 || !scheduleClock.MatchString(d.Time) {
		return errors.New("forward: monthly discovery schedule needs DayOfTheWeek 0-6, WeekOfTheMonth 1-4 and Time HH:mm")
	}
	return nil
}

// ScheduledDiscoveryDefinition creates a scheduled discovery. Forward does not send hostsToSkip or subnetsToSkip back, so a read
// of a stored one always shows them empty. Params may be SubnetScanParams, SeededDiscoveryParams or
// DirectDiscoveryParams (Forward does not schedule CLOUD). A nil Schedule runs it after every collection.
type ScheduledDiscoveryDefinition struct {
	Enabled  bool               `json:"enabled"`
	Name     string             `json:"name"`
	Params   DiscoveryParams    `json:"params"`
	Schedule *DiscoverySchedule `json:"schedule,omitempty"`
}

// ScheduledDiscovery is a stored scheduled discovery.
type ScheduledDiscovery struct {
	ID       string
	Enabled  bool
	Name     string
	Params   DiscoveryParams
	Schedule *DiscoverySchedule
}

func (d *ScheduledDiscovery) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID       Identifier         `json:"id"`
		Enabled  bool               `json:"enabled"`
		Name     string             `json:"name"`
		Params   json.RawMessage    `json:"params"`
		Schedule *DiscoverySchedule `json:"schedule"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*d = ScheduledDiscovery{ID: raw.ID.String(), Enabled: raw.Enabled, Name: raw.Name, Schedule: raw.Schedule}
	if len(raw.Params) != 0 && string(raw.Params) != "null" {
		params, err := decodeDiscoveryParams(raw.Params)
		if err != nil {
			return err
		}
		d.Params = params
	}
	return nil
}
