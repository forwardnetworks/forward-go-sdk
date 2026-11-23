package domain

import (
	"encoding/json"
	"time"
)

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

// JSONDocument is a Forward response body kept verbatim, for consumers whose
// contract is Forward's own JSON. It is an alias, not a defined type: a type
// defined from json.RawMessage loses RawMessage's (Un)MarshalJSON, so
// encoding/json decodes it as []byte (a base64 string, failing on any object)
// and encodes it as base64 too.
type JSONDocument = json.RawMessage

type UnhealthyInterfacesRequest struct{ Devices []string }
