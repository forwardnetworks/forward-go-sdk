package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// PerformanceService uploads collector performance payloads. This endpoint
// uses collector credentials, supplied per call rather than replacing the API
// credentials on the shared client.
type PerformanceService service

const SyntheticPerformanceTimeout = 10 * time.Minute

type SyntheticPerformanceRequest struct {
	SnapshotID                string
	GenerationIntervalMinutes int
	HealthyDeviceOdds         float64
	HealthyInterfaceOdds      float64
}
type SyntheticPerformanceResult map[string]int

type MetricQuery struct {
	Type            string
	Days            int
	Direction       string
	Device          string
	Interface       string
	InterfaceFilter string
	SnapshotID      string
	StartTime       string
	EndTime         string
	MaxSamples      int
}
type MetricDataPoint struct {
	Instant json.RawMessage `json:"instant"`
	Value   float64         `json:"value"`
}
type DeviceMetric struct {
	DeviceName string  `json:"deviceName"`
	Value      float64 `json:"value"`
}
type DeviceMetrics struct {
	Metrics []DeviceMetric `json:"metrics"`
}
type DeviceMetricHistory struct {
	DeviceName string            `json:"deviceName"`
	Data       []MetricDataPoint `json:"data"`
}
type DeviceMetricHistoryResponse struct {
	Metrics []DeviceMetricHistory `json:"metrics"`
}
type DeviceMetricHistoryRequest struct {
	Devices []string `json:"devices"`
}
type InterfaceMetric struct {
	DeviceName    string  `json:"deviceName"`
	InterfaceName string  `json:"interfaceName"`
	Direction     string  `json:"direction"`
	Value         float64 `json:"value"`
}
type InterfaceMetrics struct {
	Metrics []InterfaceMetric `json:"metrics"`
}
type InterfaceWithDirection struct {
	DeviceName    string `json:"deviceName"`
	InterfaceName string `json:"interfaceName"`
	Direction     string `json:"direction"`
}
type InterfaceMetricHistory struct {
	Interface InterfaceWithDirection `json:"interfaceWithDirection"`
	Data      []MetricDataPoint      `json:"data"`
}
type InterfaceMetricHistoryResponse struct {
	Metrics []InterfaceMetricHistory `json:"metrics"`
}
type InterfaceMetricHistoryRequest struct {
	Interfaces []InterfaceWithDirection `json:"interfaces"`
}
type JSONDocument json.RawMessage
type UnhealthyInterfacesRequest struct{ Devices []string }

func (s *PerformanceService) GenerateSynthetic(ctx context.Context, networkID string, input SyntheticPerformanceRequest) (SyntheticPerformanceResult, *Response, error) {
	path, err := performanceNetworkPath(s.client, networkID, "/performance")
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"op": []string{"generate"}}
	interval := input.GenerationIntervalMinutes
	if interval <= 0 {
		interval = 10
	}
	deviceOdds := input.HealthyDeviceOdds
	if deviceOdds == 0 {
		deviceOdds = .8
	}
	interfaceOdds := input.HealthyInterfaceOdds
	if interfaceOdds == 0 {
		interfaceOdds = .8
	}
	query.Set("generationIntervalMins", strconv.Itoa(interval))
	query.Set("healthyDeviceOdds", strconv.FormatFloat(deviceOdds, 'f', -1, 64))
	query.Set("healthyInterfaceOdds", strconv.FormatFloat(interfaceOdds, 'f', -1, 64))
	if strings.TrimSpace(input.SnapshotID) != "" {
		query.Set("snapshotId", strings.TrimSpace(input.SnapshotID))
	}
	path = strings.Replace(path, "/api/networks/", "/api/internal/networks/", 1) + "?" + query.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Performance.GenerateSynthetic")
	out := SyntheticPerformanceResult{}
	response, err := s.client.doWithTimeout(req, &out, true, nil, SyntheticPerformanceTimeout)
	return out, response, err
}

func (s *PerformanceService) DeviceMetrics(ctx context.Context, networkID string, query MetricQuery) (*DeviceMetrics, *Response, error) {
	out := new(DeviceMetrics)
	response, err := s.metricGet(ctx, networkID, "/device-metrics", query, out, "Performance.DeviceMetrics")
	return out, response, err
}
func (s *PerformanceService) DeviceMetricsDocument(ctx context.Context, networkID string, query MetricQuery) (JSONDocument, *Response, error) {
	var out JSONDocument
	response, err := s.metricGet(ctx, networkID, "/device-metrics", query, &out, "Performance.DeviceMetricsDocument")
	return out, response, err
}
func (s *PerformanceService) DeviceMetricHistory(ctx context.Context, networkID string, query MetricQuery, input DeviceMetricHistoryRequest) (*DeviceMetricHistoryResponse, *Response, error) {
	out := new(DeviceMetricHistoryResponse)
	response, err := s.metricPost(ctx, networkID, "/device-metrics-history", query, input, out, "Performance.DeviceMetricHistory")
	return out, response, err
}
func (s *PerformanceService) DeviceMetricHistoryDocument(ctx context.Context, networkID string, query MetricQuery, input DeviceMetricHistoryRequest) (JSONDocument, *Response, error) {
	var out JSONDocument
	response, err := s.metricPost(ctx, networkID, "/device-metrics-history", query, input, &out, "Performance.DeviceMetricHistoryDocument")
	return out, response, err
}
func (s *PerformanceService) InterfaceMetrics(ctx context.Context, networkID string, query MetricQuery) (*InterfaceMetrics, *Response, error) {
	out := new(InterfaceMetrics)
	response, err := s.metricGet(ctx, networkID, "/interface-metrics", query, out, "Performance.InterfaceMetrics")
	return out, response, err
}
func (s *PerformanceService) InterfaceMetricsDocument(ctx context.Context, networkID string, query MetricQuery) (JSONDocument, *Response, error) {
	var out JSONDocument
	response, err := s.metricGet(ctx, networkID, "/interface-metrics", query, &out, "Performance.InterfaceMetricsDocument")
	return out, response, err
}
func (s *PerformanceService) InterfaceMetricHistory(ctx context.Context, networkID string, query MetricQuery, input InterfaceMetricHistoryRequest) (*InterfaceMetricHistoryResponse, *Response, error) {
	out := new(InterfaceMetricHistoryResponse)
	response, err := s.metricPost(ctx, networkID, "/interface-metrics-history", query, input, out, "Performance.InterfaceMetricHistory")
	return out, response, err
}
func (s *PerformanceService) InterfaceMetricHistoryDocument(ctx context.Context, networkID string, query MetricQuery, input InterfaceMetricHistoryRequest) (JSONDocument, *Response, error) {
	var out JSONDocument
	response, err := s.metricPost(ctx, networkID, "/interface-metrics-history", query, input, &out, "Performance.InterfaceMetricHistoryDocument")
	return out, response, err
}
func (s *PerformanceService) UnhealthyDevices(ctx context.Context, networkID string, query MetricQuery) (JSONDocument, *Response, error) {
	var out JSONDocument
	response, err := s.metricGet(ctx, networkID, "/unhealthy-devices", query, &out, "Performance.UnhealthyDevices")
	return out, response, err
}
func (s *PerformanceService) UnhealthyInterfaces(ctx context.Context, networkID string, query MetricQuery, input UnhealthyInterfacesRequest) (JSONDocument, *Response, error) {
	var out JSONDocument
	response, err := s.metricPost(ctx, networkID, "/unhealthy-interfaces", query, input.Devices, &out, "Performance.UnhealthyInterfaces")
	return out, response, err
}

func (s *PerformanceService) metricGet(ctx context.Context, networkID, suffix string, query MetricQuery, out any, operation string) (*Response, error) {
	path, err := performanceNetworkPath(s.client, networkID, suffix)
	if err != nil {
		return nil, err
	}
	if encoded := metricQuery(query).Encode(); encoded != "" {
		path += "?" + encoded
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	return s.client.doRequired(req, out)
}
func (s *PerformanceService) metricPost(ctx context.Context, networkID, suffix string, query MetricQuery, input, out any, operation string) (*Response, error) {
	path, err := performanceNetworkPath(s.client, networkID, suffix)
	if err != nil {
		return nil, err
	}
	if encoded := metricQuery(query).Encode(); encoded != "" {
		path += "?" + encoded
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	return s.client.doRequired(req, out)
}
func performanceNetworkPath(client *Client, networkID, suffix string) (string, error) {
	networkID, err := client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + suffix, nil
}
func metricQuery(input MetricQuery) url.Values {
	q := url.Values{}
	if v := strings.TrimSpace(input.Type); v != "" {
		q.Set("type", v)
	}
	if input.Days > 0 {
		q.Set("days", strconv.Itoa(input.Days))
	}
	if v := strings.TrimSpace(input.Direction); v != "" {
		q.Set("direction", v)
	}
	if v := strings.TrimSpace(input.Device); v != "" {
		q.Set("device", v)
	}
	if v := strings.TrimSpace(input.Interface); v != "" {
		q.Set("interface", v)
	}
	if v := strings.TrimSpace(input.InterfaceFilter); v != "" {
		q.Set("interfaceFilter", v)
	}
	if v := strings.TrimSpace(input.SnapshotID); v != "" {
		q.Set("snapshotId", v)
	}
	if v := strings.TrimSpace(input.StartTime); v != "" {
		q.Set("startTime", v)
	}
	if v := strings.TrimSpace(input.EndTime); v != "" {
		q.Set("endTime", v)
	}
	if input.MaxSamples > 0 {
		q.Set("maxSamples", strconv.Itoa(input.MaxSamples))
	}
	return q
}

func (s *PerformanceService) Upload(
	ctx context.Context,
	networkID string,
	collectorUsername string,
	collectorPassword string,
	payload []byte,
) (*Response, error) {
	return s.UploadWithIdentity(ctx, networkID, CollectorIdentity{Username: collectorUsername, AuthorizationKey: collectorPassword}, payload)
}

// UploadWithIdentity uploads performance data using the collector identity
// returned by Collectors.Register.
func (s *PerformanceService) UploadWithIdentity(
	ctx context.Context,
	networkID string,
	identity CollectorIdentity,
	payload []byte,
) (*Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	collectorUsername := strings.TrimSpace(identity.Username)
	collectorPassword := identity.AuthorizationKey
	if collectorUsername == "" || collectorPassword == "" {
		return nil, errors.New("forward: collector username and password are required")
	}
	path, _ := networkPath(networkID)
	req, err := s.client.newScopedRequest(ctx, http.MethodPost, path+"/performance", bytes.NewReader(payload), pathScopeAPI, &requestAuth{mode: AuthModeCollector, username: collectorUsername, password: collectorPassword})
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Performance.UploadWithIdentity")
	req.Header.Set("Content-Type", "application/octet-stream")
	return s.client.Do(req, nil)
}
