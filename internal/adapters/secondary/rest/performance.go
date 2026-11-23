package rest

import (
	"bytes"
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// PerformanceService uploads collector performance payloads. This endpoint
// uses collector credentials, supplied per call rather than replacing the API
// credentials on the shared client.
type Performance service

func (s *Performance) GenerateSynthetic(ctx context.Context, networkID string, input ports.SyntheticPerformanceRequest) (ports.SyntheticPerformanceResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := ports.SyntheticPerformanceResult{}
	response, err := s.client.doWithTimeout(req, &out, true, nil, ports.SyntheticPerformanceTimeout)
	return out, response, err
}

func (s *Performance) DeviceMetrics(ctx context.Context, networkID string, query ports.MetricQuery) (*ports.DeviceMetrics, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.DeviceMetrics)
	response, err := s.metricGet(ctx, networkID, "/device-metrics", query, out, "Performance.DeviceMetrics")
	return out, response, err
}

func (s *Performance) DeviceMetricsDocument(ctx context.Context, networkID string, query ports.MetricQuery) (ports.JSONDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var out ports.JSONDocument
	response, err := s.metricGet(ctx, networkID, "/device-metrics", query, &out, "Performance.DeviceMetricsDocument")
	return out, response, err
}

func (s *Performance) DeviceMetricHistory(ctx context.Context, networkID string, query ports.MetricQuery, input ports.DeviceMetricHistoryRequest) (*ports.DeviceMetricHistoryResponse, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.DeviceMetricHistoryResponse)
	response, err := s.metricPost(ctx, networkID, "/device-metrics-history", query, input, out, "Performance.DeviceMetricHistory")
	return out, response, err
}

func (s *Performance) DeviceMetricHistoryDocument(ctx context.Context, networkID string, query ports.MetricQuery, input ports.DeviceMetricHistoryRequest) (ports.JSONDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var out ports.JSONDocument
	response, err := s.metricPost(ctx, networkID, "/device-metrics-history", query, input, &out, "Performance.DeviceMetricHistoryDocument")
	return out, response, err
}

func (s *Performance) InterfaceMetrics(ctx context.Context, networkID string, query ports.MetricQuery) (*ports.InterfaceMetrics, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.InterfaceMetrics)
	response, err := s.metricGet(ctx, networkID, "/interface-metrics", query, out, "Performance.InterfaceMetrics")
	return out, response, err
}

func (s *Performance) InterfaceMetricsDocument(ctx context.Context, networkID string, query ports.MetricQuery) (ports.JSONDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var out ports.JSONDocument
	response, err := s.metricGet(ctx, networkID, "/interface-metrics", query, &out, "Performance.InterfaceMetricsDocument")
	return out, response, err
}

func (s *Performance) InterfaceMetricHistory(ctx context.Context, networkID string, query ports.MetricQuery, input ports.InterfaceMetricHistoryRequest) (*ports.InterfaceMetricHistoryResponse, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.InterfaceMetricHistoryResponse)
	response, err := s.metricPost(ctx, networkID, "/interface-metrics-history", query, input, out, "Performance.InterfaceMetricHistory")
	return out, response, err
}

func (s *Performance) InterfaceMetricHistoryDocument(ctx context.Context, networkID string, query ports.MetricQuery, input ports.InterfaceMetricHistoryRequest) (ports.JSONDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var out ports.JSONDocument
	response, err := s.metricPost(ctx, networkID, "/interface-metrics-history", query, input, &out, "Performance.InterfaceMetricHistoryDocument")
	return out, response, err
}

func (s *Performance) UnhealthyDevices(ctx context.Context, networkID string, query ports.MetricQuery) (ports.JSONDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var out ports.JSONDocument
	response, err := s.metricGet(ctx, networkID, "/unhealthy-devices", query, &out, "Performance.UnhealthyDevices")
	return out, response, err
}

func (s *Performance) UnhealthyInterfaces(ctx context.Context, networkID string, query ports.MetricQuery, input ports.UnhealthyInterfacesRequest) (ports.JSONDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var out ports.JSONDocument
	response, err := s.metricPost(ctx, networkID, "/unhealthy-interfaces", query, input.Devices, &out, "Performance.UnhealthyInterfaces")
	return out, response, err
}

func (s *Performance) metricGet(ctx context.Context, networkID, suffix string, query ports.MetricQuery, out any, operation string) (*ports.Response, error) {
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

func (s *Performance) metricPost(ctx context.Context, networkID, suffix string, query ports.MetricQuery, input, out any, operation string) (*ports.Response, error) {
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

func metricQuery(input ports.MetricQuery) url.Values {
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

// UploadWithIdentity uploads performance data using the collector identity
// returned by Collectors.Register. payload is a serialized PerfDataPB
// (PerfProtos.proto in Forward's client-structs); the SDK has no protobuf
// dependency, so encoding it is the caller's. As far as the
// controller and PerformanceService.save show, Forward checks that the
// collector belongs to the network's organization and nothing else: nothing
// there checks that the network is the collector's default, that the device
// names exist, or that this collector is the one a device names. Cumulative
// interface counters are expected; Forward derives rates and utilization from
// the difference to the device's previous sample. A sample whose CPU or
// memory is above 1 is dropped for the whole device; one interface whose
// utilization is above 1 is dropped on its own. Within a batch
// keep one device's entries in time order. Forward answers 204.
func (s *Performance) UploadWithIdentity(
	ctx context.Context,
	networkID string,
	identity ports.CollectorIdentity,
	payload []byte,
) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
	req, err := s.client.newScopedRequest(ctx, http.MethodPost, path+"/performance", bytes.NewReader(payload), pathScopeAPI, &requestAuth{mode: ports.AuthModeCollector, username: collectorUsername, password: collectorPassword})
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Performance.UploadWithIdentity")
	req.Header.Set("Content-Type", "application/octet-stream")
	return s.client.Do(req, nil)
}
