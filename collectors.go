package forward

import (
	"context"
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

type CollectorAttachment struct {
	IsSet             bool       `json:"isSet"`
	CollectorID       Identifier `json:"collectorId,omitempty"`
	CollectorUsername string     `json:"collectorUsername,omitempty"`
	CollectorName     string     `json:"collectorName,omitempty"`
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

func (s *CollectorsService) Get(ctx context.Context, collectorIDOrName string) (*Collector, *Response, error) {
	value := strings.TrimSpace(collectorIDOrName)
	if value == "" {
		return nil, nil, errors.New("forward: collector ID or name is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/collectors/"+url.PathEscape(value), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.Get")
	out := new(Collector)
	response, err := s.client.doRequired(req, out)
	return out, response, err
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
	if isStatus(err, http.StatusNotFound) {
		return response, nil
	}
	return response, err
}

func (s *CollectorsService) Attachment(ctx context.Context, networkID string) (*CollectorAttachment, *Response, error) {
	path, err := s.networkPath(networkID, "/collector/status")
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

// StartLegacy starts the legacy network collection route. A typed conflict
// for an already-running collection is returned as success, matching the
// call sites that treat the operation as idempotent.
func (s *CollectorsService) StartLegacy(ctx context.Context, networkID string) (*Response, error) {
	path, err := s.networkPath(networkID, "/startcollection")
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Collectors.StartLegacy")
	response, err := s.client.Do(req, nil)
	if errors.Is(err, ErrCollectionAlreadyInProgress) {
		return response, nil
	}
	return response, err
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
