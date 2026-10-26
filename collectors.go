package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// CollectorsService manages collector registration and network attachment.
type CollectorsService service

type Collector struct {
	ID                Identifier `json:"id"`
	Name              string     `json:"name"`
	Username          string     `json:"username"`
	IsDefault         bool       `json:"isDefault,omitempty"`
	Status            string     `json:"status,omitempty"`
	Connected         bool       `json:"connected,omitempty"`
	LastConnectedAt   int64      `json:"lastConnectedAt,omitempty"`
	UpdatedAt         int64      `json:"updatedAt,omitempty"`
	ConnectionStatus  string     `json:"connectionStatus,omitempty"`
	Version           string     `json:"version,omitempty"`
	UpdateStatus      string     `json:"updateStatus,omitempty"`
	CollectorUpdating bool       `json:"collectorUpdating,omitempty"`
	ExternalIP        string     `json:"externalIp,omitempty"`
	InternalIPs       []string   `json:"internalIps,omitempty"`
	// Features is what the collector reported at its last connection; nil when it never connected.
	Features *CollectorFeatures `json:"features,omitempty"`
}

// CollectorFeatures is the collector's self-reported capability set. Updatable is true when the collector runs as a
// service Forward can restart (supportsAutoStart), which a remote upgrade needs.
type CollectorFeatures struct {
	Latest    int  `json:"latest"`
	Updatable bool `json:"updatable,omitempty"`
}

// UpgradeBlocker says why Forward would refuse Upgrade for this collector, from the fields a list read carries, or ""
// when nothing visible blocks it. It mirrors CollectorUpgradeService.validateCollectorIsRemotelyUpgradeable and is
// advisory: it does not check for a bundled on-prem collector (username "client") or an on-prem deployment in
// BUNDLED_ONLY mode, and Forward answers those itself (400, or 409 while one is in progress).
func (c Collector) UpgradeBlocker() string {
	switch {
	case c.CollectorUpdating:
		return "an upgrade is already queued or running"
	case c.ConnectionStatus == "NEVER_CONNECTED":
		return "the collector has never connected"
	case c.ConnectionStatus == "DISCONNECTED":
		return "the collector is offline"
	case c.Features == nil || !c.Features.Updatable:
		return "the collector is not remotely upgradeable (it is not installed as a service Forward can restart)"
	case c.UpdateStatus == "":
		return "the collector reports no version (development mode)"
	case c.UpdateStatus == "UP_TO_DATE":
		return "the collector is up to date"
	case c.UpdateStatus == "END_OF_LIFE":
		return "the collector is at end of life and must be reinstalled by hand"
	}
	return ""
}

type CollectorRegistrationRequest struct {
	CollectorName string `json:"collectorName"`
}

// CollectorRegistration contains the distinct collector authorization
// identity returned by registration. Callers must deploy AuthorizationKey as
// the collector credential; the API user's credential is not interchangeable.
type CollectorRegistration struct {
	ID               Identifier `json:"id"`
	Name             string     `json:"name"`
	Username         string     `json:"username"`
	AuthorizationKey string     `json:"authorizationKey"`
}

// Identity is the collector's own credential, for collector-authenticated calls
// such as Performance.UploadWithIdentity. AuthorizationKey of the identity is the
// collector PASSWORD: Forward's authorizationKey is the whole installation key
// ("collector-x:password", or the V3 base64), so the password is decoded out of
// it. A value that is not a recognisable key is passed through unchanged.
func (r CollectorRegistration) Identity() CollectorIdentity {
	password := r.AuthorizationKey
	if key, err := ParseCollectorAuthKey(r.AuthorizationKey); err == nil {
		password = key.Password
	}
	return CollectorIdentity{Username: strings.TrimSpace(r.Username), AuthorizationKey: password}
}

// CollectorAttachment reports whether a Collector is attached to a network.
//
// The wire shape is Forward's CollectorWithStatus (id/name/username/...),
// decoded straight off GET /api/networks/{id}/collector -- NOT the legacy
// "isSet"/"busyStatus" CollectorState shape. Forward retired the old
// GET .../collector/status route server-side 2026-09-13 (FWD-52021,
// "Retire legacy CollectorState, CollectorStateService and CollectorStatus");
// it now 404s unconditionally, for every network, whether or not a collector
// is attached. The live replacement returns 200 with an empty JSON object
// ({}) when no collector is attached, so IsSet is derived from
// CollectorUsername being present, not decoded from a wire field -- there is
// no such field anymore.
type CollectorAttachment struct {
	CollectorID       Identifier `json:"id,omitempty"`
	CollectorUsername string     `json:"username,omitempty"`
	CollectorName     string     `json:"name,omitempty"`
	ConnectionStatus  string     `json:"connectionStatus,omitempty"`
	UpdateStatus      string     `json:"updateStatus,omitempty"`
	IsSet             bool       `json:"-"`
}

type CollectorAttachmentRequest struct {
	Username string `json:"username"`
}

type PerformanceCollectionSettings struct {
	Enabled bool `json:"enabled"`
}

type OrgCollectionSettingsPatch struct {
	MaxDeviceAuthNPerSecond     *int `json:"maxDeviceAuthNPerSecond,omitempty"`
	MaxScanConnectionsPerSecond *int `json:"maxScanConnectionsPerSecond,omitempty"`
	DeviceCollectionTimeoutMins *int `json:"deviceCollectionTimeoutMinutes,omitempty"`
	CommandDelayMS              *int `json:"commandDelayMs,omitempty"`
	PerDeviceConcurrencyBoost   *int `json:"perDeviceConcurrencyBoost,omitempty"`
	// PerDeviceSNMPConcurrency is the concurrent SNMP transactions allowed per device (1 to 100, anything else is a 400; Forward's
	// default is DefaultPerDeviceSNMPConcurrency).
	PerDeviceSNMPConcurrency *int `json:"perDeviceSnmpConcurrency,omitempty"`
	// The three collection retry properties are one stored CollectionRetryConfig; Forward merges whichever are present into
	// it and keeps the others, so setting one does not reset the rest. CollectionRetries counts retries AFTER the first
	// attempt (0 turns retries off); the delays double from CollectionRetryDelayMillis up to CollectionMaxRetryDelayMillis.
	// Negative values are a 400. Defaults: DefaultCollectionRetries, DefaultCollectionRetryDelayMillis,
	// DefaultCollectionMaxRetryDelayMillis.
	CollectionRetries             *int `json:"collectionRetries,omitempty"`
	CollectionRetryDelayMillis    *int `json:"collectionRetryDelayMillis,omitempty"`
	CollectionMaxRetryDelayMillis *int `json:"collectionMaxRetryDelayMillis,omitempty"`
	// Clear names wire properties to send as an explicit JSON null, which RESETS them to unset so Forward's default applies.
	// That is not the same as setting the default's value: a stored 15000 stays 15000 if Forward's default later changes, and
	// reads back as set (a non-nil field), whereas a cleared property reads back nil. Clearable: maxDeviceAuthNPerSecond,
	// maxScanConnectionsPerSecond, deviceCollectionTimeoutMinutes, commandDelayMs, perDeviceConcurrencyBoost,
	// perDeviceSnmpConcurrency, collectionRetries, collectionRetryDelayMillis, collectionMaxRetryDelayMillis,
	// maxCommandAuthZPerSecond, deviceDiscoveryTimeoutMinutes, generalJobTimeoutMinutes, snapshotCollectionTimeoutMinutes
	// (the last four have no field here, so Clear is the only way to reset them). Naming a
	// property that is also set is an error.
	Clear []string `json:"-"`
	// DisabledCommands turns built-in collection commands off org-wide, per
	// device type (the key is Forward's device type name as the classic device
	// "type" field carries it, e.g. "panos_ssh", "cisco_ios_xe_ssh", "f5_ssh").
	// nil leaves the setting alone. A non-nil map REPLACES the whole stored map
	// (OrgCollectionSettingsPatch.disabledCommands is a JsonProp applied as a
	// whole), so adding one command is read-modify-write: read
	// OrgCollectionSettings.DisabledCommands, add to a copy, send it. A non-nil
	// EMPTY map clears every disabled command. Each entry needs a command and
	// both version bounds; PatchOrganizationSettings checks that before sending.
	// Forward's release notes list setting disabled commands through this API as
	// new in 26.8.3 (BUG-27961). A build without it most likely refuses the
	// property as unrecognised (a 400: Forward's request mapper rejects unknown
	// properties); that was not tried on an older build. A pointer to a nil map
	// sends null, which also clears every disabled command; prefer an empty map.
	// There is no concurrency guard, so two editors can overwrite each other.
	DisabledCommands *map[string][]DisabledCommand `json:"disabledCommands,omitempty"`
}

var orgCollectionSettingsClearable = map[string]bool{
	"maxDeviceAuthNPerSecond": true, "maxScanConnectionsPerSecond": true, "deviceCollectionTimeoutMinutes": true,
	"commandDelayMs": true, "perDeviceConcurrencyBoost": true, "perDeviceSnmpConcurrency": true,
	"collectionRetries": true, "collectionRetryDelayMillis": true, "collectionMaxRetryDelayMillis": true,
	"maxCommandAuthZPerSecond": true, "deviceDiscoveryTimeoutMinutes": true, "generalJobTimeoutMinutes": true,
	"snapshotCollectionTimeoutMinutes": true,
}

// MarshalJSON writes the set properties and a JSON null for each name in Clear.
func (p OrgCollectionSettingsPatch) MarshalJSON() ([]byte, error) {
	type plain OrgCollectionSettingsPatch
	raw, err := json.Marshal(plain(p))
	if err != nil {
		return nil, err
	}
	if len(p.Clear) == 0 {
		return raw, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for _, name := range p.Clear {
		if !orgCollectionSettingsClearable[name] {
			return nil, fmt.Errorf("forward: organization collection setting %q cannot be cleared with null", name)
		}
		if value, set := fields[name]; set && string(value) != "null" {
			return nil, fmt.Errorf("forward: organization collection settings patch both sets and clears %q", name)
		}
		fields[name] = json.RawMessage("null")
	}
	return json.Marshal(fields)
}

// DisabledCommand switches one built-in collection command off for a device
// type and a range of device software versions (CollectionDisabledCommand).
// Command is a case-insensitive Java regular expression matched against the
// command Forward would run (not checked here: Java and Go syntax differ); the range is inclusive and both bounds are required.
type DisabledCommand struct {
	Command string                 `json:"command"`
	Version DisabledCommandVersion `json:"version"`
}

// DisabledCommandVersion is the inclusive range of software versions a
// DisabledCommand applies to (ReleaseVersion strings such as "1.0" to "99.0").
type DisabledCommandVersion struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

func validateDisabledCommands(commands map[string][]DisabledCommand) error {
	for deviceType, list := range commands {
		if strings.TrimSpace(deviceType) == "" {
			return errors.New("forward: a disabled-commands device type must not be empty")
		}
		for i, command := range list {
			if strings.TrimSpace(command.Command) == "" || strings.TrimSpace(command.Version.Start) == "" || strings.TrimSpace(command.Version.End) == "" {
				return fmt.Errorf("forward: disabled command %d for %q needs a command and both version.start and version.end", i, deviceType)
			}
		}
	}
	return nil
}

type CollectorCollectionSettingsPatch struct {
	Concurrency               *int `json:"concurrency,omitempty"`
	SNMPCollectionConcurrency *int `json:"snmpCollectionConcurrency,omitempty"`
}

func (s *CollectorsService) List(ctx context.Context) ([]Collector, *Response, error) {
	result := listResponse[Collector]{Keys: []string{"collectors", "items", "data"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/collectors", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *CollectorsService) Register(ctx context.Context, input CollectorRegistrationRequest) (*CollectorRegistration, *Response, error) {
	input.CollectorName = strings.TrimSpace(input.CollectorName)
	if input.CollectorName == "" {
		return nil, nil, errors.New("forward: collector name is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/collectors", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.Register")
	out := new(CollectorRegistration)
	response, err := s.client.doRequired(req, out)
	if err == nil && (out.ID == "" || strings.TrimSpace(out.Username) == "" || out.AuthorizationKey == "") {
		err = errors.New("forward: collector registration returned an incomplete authorization identity")
	}
	return out, response, err
}

func (s *CollectorsService) Delete(ctx context.Context, collectorIDOrName string) (*Response, error) {
	value := strings.TrimSpace(collectorIDOrName)
	if value == "" {
		return nil, errors.New("forward: collector ID or name is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/collectors/"+url.PathEscape(value), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.Delete")
	response, err := s.client.Do(req, nil)
	if isGone(err) {
		return response, nil
	}
	return response, err
}

func (s *CollectorsService) Attachment(ctx context.Context, networkID string) (*CollectorAttachment, *Response, error) {
	// /collector, not /collector/status -- see the CollectorAttachment doc
	// comment. The /status route is gone server-side and 404s regardless of
	// network or attachment state.
	path, err := s.networkPath(networkID, "/collector")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.Attachment")
	out := new(CollectorAttachment)
	response, err := s.client.doRequired(req, out)
	if err == nil && out != nil {
		out.IsSet = strings.TrimSpace(out.CollectorUsername) != ""
	}
	return out, response, err
}

// Attach attaches the collector with the given username to a network. PUT /api/networks/{networkId}/collector
// (JSON {"username": ...}). The username is the collector's (for example "collector-7ddceb66"), not its name or id.
// Check the result with Attachment, which reads the same route.
func (s *CollectorsService) Attach(ctx context.Context, networkID string, input CollectorAttachmentRequest) (*Response, error) {
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" {
		return nil, errors.New("forward: collector username is required")
	}
	path, err := s.networkPath(networkID, "/collector")
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path, input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.Attach")
	return s.client.Do(req, nil)
}

func (s *CollectorsService) SetPerformanceCollection(ctx context.Context, networkID string, settings PerformanceCollectionSettings) (*Response, error) {
	path, err := s.networkPath(networkID, "/performance/settings")
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, settings)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.SetPerformanceCollection")
	return s.client.Do(req, nil)
}

func (s *CollectorsService) PatchOrganizationSettings(ctx context.Context, patch OrgCollectionSettingsPatch) (*Response, error) {
	if patch.DisabledCommands != nil {
		if err := validateDisabledCommands(*patch.DisabledCommands); err != nil {
			return nil, err
		}
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/collection-settings", patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.PatchOrganizationSettings")
	return s.client.Do(req, nil)
}

func (s *CollectorsService) PatchSettings(ctx context.Context, collectorID string, patch CollectorCollectionSettingsPatch) (*Response, error) {
	collectorID = strings.TrimSpace(collectorID)
	if collectorID == "" {
		return nil, errors.New("forward: collector ID is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/collectors/"+url.PathEscape(collectorID)+"/collection-settings", patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.PatchSettings")
	return s.client.Do(req, nil)
}

func (s *CollectorsService) networkPath(networkID, suffix string) (string, error) {
	return s.client.networkSubPath(networkID, strings.TrimPrefix(suffix, "/"))
}

// Forward's defaults for collection settings a user has not set
// (CollectorCollectionSettings and OrgCollectionSettings in client-structs).
// The reads return only what is stored, with nil for "default applies"; these
// are what Forward then uses, for comparison and for the Effective methods.
const (
	DefaultCollectorConcurrency             = 128 // devices collected at once; at most 1024
	DefaultCollectorSNMPConcurrency         = 64  // at most 1024
	DefaultCollectorVCenterConcurrency      = 1   // at most 100
	DefaultMaxCommandAuthZPerSecond         = 1000
	DefaultMaxDeviceAuthNPerSecond          = 1000
	DefaultMaxScanConnectionsPerSecond      = 2000
	DefaultPerDeviceSNMPConcurrency         = 4
	DefaultDeviceCollectionTimeoutMinutes   = 3 * 60
	DefaultDeviceDiscoveryTimeoutMinutes    = 6 * 60
	DefaultGeneralJobTimeoutMinutes         = 20
	DefaultSnapshotCollectionTimeoutMinutes = 6 * 60
	DefaultCollectionRetryDelayMillis       = 15_000
	DefaultCollectionMaxRetryDelayMillis    = 60_000
	DefaultCollectionRetries                = 2
)

// CollectorCollectionSettings is what is stored for one collector. A nil field
// means nothing is set and Forward uses its default (Default... constants);
// the Effective methods apply it explicitly.
type CollectorCollectionSettings struct {
	Concurrency               *int `json:"concurrency,omitempty"`
	SNMPCollectionConcurrency *int `json:"snmpCollectionConcurrency,omitempty"`
	VCenterConcurrency        *int `json:"vcenterConcurrency,omitempty"`
}

// EffectiveConcurrency is Concurrency, or DefaultCollectorConcurrency when unset.
func (s CollectorCollectionSettings) EffectiveConcurrency() int {
	return intOrDefault(s.Concurrency, DefaultCollectorConcurrency)
}

// EffectiveSNMPCollectionConcurrency is SNMPCollectionConcurrency, or its default when unset.
func (s CollectorCollectionSettings) EffectiveSNMPCollectionConcurrency() int {
	return intOrDefault(s.SNMPCollectionConcurrency, DefaultCollectorSNMPConcurrency)
}

// EffectiveVCenterConcurrency is VCenterConcurrency, or its default when unset.
func (s CollectorCollectionSettings) EffectiveVCenterConcurrency() int {
	return intOrDefault(s.VCenterConcurrency, DefaultCollectorVCenterConcurrency)
}

// GetSettings returns the collection settings stored for a collector: only
// what was set, so a nil field is a default, not zero. GET
// /api/collectors/{collectorId}/collection-settings (CollectionController;
// VIEW_COLLECTORS). Preview: not in the published spec.
func (s *CollectorsService) GetSettings(ctx context.Context, collectorID string) (*CollectorCollectionSettings, *Response, error) {
	if collectorID = strings.TrimSpace(collectorID); collectorID == "" {
		return nil, nil, errors.New("forward: collector ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/collectors/"+url.PathEscape(collectorID)+"/collection-settings", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.GetSettings")
	out := new(CollectorCollectionSettings)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// OrgCollectionSettings is what is stored for the whole organization's
// collectors. A nil field (or empty list) means nothing is set and Forward uses
// its default: the Default... constants where there is one; none for
// PerDeviceConcurrencyBoost (0) and CommandDelayMS (no delay). Timeouts are in
// the minutes Forward serializes. The settings this type does not model
// (the ribRoute* limits, the redaction settings) are in Raw,
// with every other key of the response.
type OrgCollectionSettings struct {
	MaxCommandAuthZPerSecond      *int     `json:"maxCommandAuthZPerSecond,omitempty"`
	MaxDeviceAuthNPerSecond       *int     `json:"maxDeviceAuthNPerSecond,omitempty"`
	MaxScanConnectionsPerSecond   *int     `json:"maxScanConnectionsPerSecond,omitempty"`
	PerDeviceConcurrencyBoost     *int     `json:"perDeviceConcurrencyBoost,omitempty"`
	PerDeviceSNMPConcurrency      *int     `json:"perDeviceSnmpConcurrency,omitempty"`
	DeviceCollectionTimeoutMins   *int     `json:"deviceCollectionTimeoutMinutes,omitempty"`
	DeviceDiscoveryTimeoutMins    *int     `json:"deviceDiscoveryTimeoutMinutes,omitempty"`
	GeneralJobTimeoutMins         *int     `json:"generalJobTimeoutMinutes,omitempty"`
	SnapshotCollectionTimeoutMins *int     `json:"snapshotCollectionTimeoutMinutes,omitempty"`
	CommandDelayMS                *int     `json:"commandDelayMs,omitempty"`
	CollectionRetryDelayMillis    *int     `json:"collectionRetryDelayMillis,omitempty"`
	CollectionMaxRetryDelayMillis *int     `json:"collectionMaxRetryDelayMillis,omitempty"`
	CollectionRetries             *int     `json:"collectionRetries,omitempty"`
	EnableBetaCommands            *bool    `json:"enableBetaCommands,omitempty"`
	DisableCommandOutputLogging   *bool    `json:"disableCommandOutputLogging,omitempty"`
	EnableSNMPCredDiscovery       *bool    `json:"enableSnmpCredDiscovery,omitempty"`
	PasswordPrompts               []string `json:"passwordPrompts,omitempty"`
	CollectFullConfigDeviceTypes  []string `json:"collectFullConfigDeviceTypes,omitempty"`
	// DisabledCommands is the built-in collection commands switched off org-wide,
	// by device type name (see OrgCollectionSettingsPatch.DisabledCommands); nil
	// when none (Forward omits an empty map). How far back the read side goes is
	// unknown; the PATCH side is 26.8.3 and later.
	DisabledCommands map[string][]DisabledCommand `json:"disabledCommands,omitempty"`

	// Raw is the whole response object.
	Raw map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the modelled fields and keeps the whole object in Raw.
func (s *OrgCollectionSettings) UnmarshalJSON(data []byte) error {
	type plain OrgCollectionSettings
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = OrgCollectionSettings(value)
	s.Raw = raw
	return nil
}

// EffectiveMaxDeviceAuthNPerSecond is MaxDeviceAuthNPerSecond, or its default when unset.
func (s OrgCollectionSettings) EffectiveMaxDeviceAuthNPerSecond() int {
	return intOrDefault(s.MaxDeviceAuthNPerSecond, DefaultMaxDeviceAuthNPerSecond)
}

// EffectiveMaxScanConnectionsPerSecond is MaxScanConnectionsPerSecond, or its default when unset.
func (s OrgCollectionSettings) EffectiveMaxScanConnectionsPerSecond() int {
	return intOrDefault(s.MaxScanConnectionsPerSecond, DefaultMaxScanConnectionsPerSecond)
}

// EffectivePerDeviceConcurrencyBoost is PerDeviceConcurrencyBoost, or 0 when unset.
func (s OrgCollectionSettings) EffectivePerDeviceConcurrencyBoost() int {
	return intOrDefault(s.PerDeviceConcurrencyBoost, 0)
}

// EffectiveDeviceCollectionTimeoutMinutes is DeviceCollectionTimeoutMins, or its default when unset.
func (s OrgCollectionSettings) EffectiveDeviceCollectionTimeoutMinutes() int {
	return intOrDefault(s.DeviceCollectionTimeoutMins, DefaultDeviceCollectionTimeoutMinutes)
}

// EffectiveSnapshotCollectionTimeoutMinutes is SnapshotCollectionTimeoutMins, or its default when unset.
func (s OrgCollectionSettings) EffectiveSnapshotCollectionTimeoutMinutes() int {
	return intOrDefault(s.SnapshotCollectionTimeoutMins, DefaultSnapshotCollectionTimeoutMinutes)
}

func intOrDefault(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

// GetOrganizationSettings returns the collection settings stored for the
// organization's collectors: only what was set, so a nil field is a default,
// not zero. GET /api/collection-settings (CollectionController;
// VIEW_ORG_COLLECTION_SETTINGS). The sibling ?for=collector form is the
// collectors' own binary feed, not this. Preview: not in the published spec.
func (s *CollectorsService) GetOrganizationSettings(ctx context.Context) (*OrgCollectionSettings, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/collection-settings", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.GetOrganizationSettings")
	out := new(OrgCollectionSettings)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Upgrade asks Forward to upgrade one collector to the current release and returns once the upgrade is queued, not
// finished. Poll List until CollectorUpdating clears, then read UpdateStatus and Version: UP_TO_DATE means it worked,
// while a failed or timed-out upgrade also clears CollectorUpdating and leaves UpdateStatus OUTDATED. The upgrade is a
// blocking task, so it first waits for a collection running on that collector to finish (Forward gives the task 15
// minutes), then restarts the collector: collections queued behind it fail with "Collector version changed". POST
// /api/collectors/{collectorName}?action=upgrade (CollectorSetupController.upgradeCollector; MANAGE_COLLECTORS, org
// admin; Preview: not in the published spec). Forward answers 400 when the collector is offline, never connected, not
// remotely upgradeable, already up to date, at end of life, bundled with an on-prem deployment, or upgrades are off
// (BUNDLED_ONLY); 404 for an unknown name; and 409 when an upgrade is already in progress. See Collector.UpgradeBlocker
// for the checks that can be made first.
func (s *CollectorsService) Upgrade(ctx context.Context, collectorName string) (*Response, error) {
	collectorName = strings.TrimSpace(collectorName)
	if collectorName == "" {
		return nil, errors.New("forward: collector name is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/collectors/"+url.PathEscape(collectorName)+"?action=upgrade", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.Upgrade")
	return s.client.Do(req, nil)
}

// UpgradeAll queues an upgrade for every collector in the org that can be upgraded remotely (online, outdated or
// unsupported but not end of life, not bundled, installed as a service) and returns their names; collectors with an
// upgrade already queued are left out. An empty list means nothing qualified. POST /api/collectors?action=upgrade
// (CollectorSetupController.upgradeCollectors; MANAGE_COLLECTORS; Preview: not in the published spec). The order of
// the names is unspecified. Like Upgrade, it returns when queued, with the same polling and restart behaviour.
func (s *CollectorsService) UpgradeAll(ctx context.Context) ([]string, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/collectors?action=upgrade", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.UpgradeAll")
	var names []string
	response, err := s.client.doRequired(req, &names)
	if err != nil {
		return nil, response, err
	}
	if names == nil {
		names = []string{}
	}
	return names, response, err
}
