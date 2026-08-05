package forward

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (s *SourceTestStatus) UnmarshalJSON(data []byte) error {
	type plain SourceTestStatus
	var direct plain
	_ = json.Unmarshal(data, &direct)
	*s = SourceTestStatus(direct)
	var item map[string]any
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	if s.Name == "" {
		s.Name = firstAnyString(item, "name", "deviceName")
	}
	if s.ConnectivityError == "" {
		s.ConnectivityError = normalizeErrorCode(firstAnyString(item, "connectivityError"))
	}
	if s.ConnectivityErrorRaw == "" {
		s.ConnectivityErrorRaw = firstAnyString(item, "connectivityErrorRaw")
	}
	if s.ErrorPhase == "" {
		s.ErrorPhase = firstAnyString(item, "errorPhase")
	}
	raw := item["testResult"]
	if raw == nil {
		raw = item["test_result"]
	}
	if result, ok := raw.(map[string]any); ok && len(result) != 0 {
		s.TestResultPresent = true
		s.ConnectivityError = firstAnyString(result, "error", "connectivityTestError", "errorCode")
		s.ConnectivityErrorRaw = s.ConnectivityError
		if errorObject, ok := result["error"].(map[string]any); ok {
			if s.ConnectivityError == "" {
				s.ConnectivityError = firstAnyString(errorObject, "name", "code", "value")
			}
			if s.ConnectivityErrorRaw == "" {
				s.ConnectivityErrorRaw = firstAnyString(errorObject, "label", "description", "message")
			}
		}
		s.ConnectivityError = normalizeErrorCode(s.ConnectivityError)
		s.ErrorPhase = firstAnyString(result, "errorPhase", "phase")
	}
	if raw := item["snmpCollectionStatus"]; raw != nil {
		s.SNMPCollectionStatus = anyString(raw)
		if object, ok := raw.(map[string]any); ok {
			s.SNMPCollectionStatus = firstAnyString(object, "status", "state", "message")
		}
	}
	return nil
}

func (s *DeviceCollectionStatus) UnmarshalJSON(data []byte) error {
	type plain DeviceCollectionStatus
	var direct plain
	_ = json.Unmarshal(data, &direct)
	*s = DeviceCollectionStatus(direct)
	var item map[string]any
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	if s.Name == "" {
		s.Name = firstAnyString(item, "deviceName", "name")
	}
	if s.CollectionStatus == "" {
		s.CollectionStatus = firstAnyString(item, "collectionStatus", "status", "state")
	}
	if raw := item["collectionError"]; raw != nil {
		s.CollectionError = anyString(raw)
		if object, ok := raw.(map[string]any); ok {
			s.CollectionError = firstAnyString(object, "name", "code", "value", "error")
		}
		s.CollectionError = normalizeErrorCode(s.CollectionError)
	}
	return nil
}

func firstAnyString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := anyString(item[key]); value != "" {
			return value
		}
	}
	return ""
}
func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	default:
		return ""
	}
}
func normalizeErrorCode(value string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), " ", "_"))
}
