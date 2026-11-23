package domain

import (
	"encoding/json"
)

// MaxConnectivityTestDevices is the most devices one BulkStart or BulkStop call accepts. Forward has no limit of its own, and
// a call that names a large set queues a probe of each from one collector; a caller that really means more splits it into calls
// with time between them and watches List, rather than this SDK queueing thousands of logins in one request.
const MaxConnectivityTestDevices = 100

// ConnectivityTestResult is one device's stored connectivity test result: its name and what the test found, decoded as
// DeviceTestResult (error code and phase, discovered type, controllers, host IPs, the times). An empty Error with an EndTime is
// a pass; an empty Error and EndTime is a result with nothing in it.
type ConnectivityTestResult struct {
	DeviceName string `json:"deviceName"`
	DeviceTestResult
}

func (r *ConnectivityTestResult) UnmarshalJSON(data []byte) error {
	var head struct {
		DeviceName string `json:"deviceName"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	var result DeviceTestResult
	if err := result.UnmarshalJSON(data); err != nil {
		return err
	}
	*r = ConnectivityTestResult{DeviceName: head.DeviceName, DeviceTestResult: result}
	return nil
}
