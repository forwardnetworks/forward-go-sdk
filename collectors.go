package forward

import (
	"context"
	"encoding/json"
	"errors"
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

func (r CollectorRegistration) Identity() CollectorIdentity {
	return CollectorIdentity{Username: strings.TrimSpace(r.Username), AuthorizationKey: r.AuthorizationKey}
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
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + suffix, nil
}

func isStatus(err error, status int) bool {
	var responseError *ErrorResponse
	return errors.As(err, &responseError) && responseError.Response != nil && responseError.Response.StatusCode == status
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
// (disabledCommands, the ribRoute* limits, the redaction settings) are in Raw,
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
