package forward

import (
	"encoding/json"
	"testing"
)

// Failure triage needs to know how old a stored test result is (Forward keeps
// the last one until the next test replaces it) and who created the device and
// when. The times live inside testResult and the attribution is unwrapped into
// the device object. A device created before Forward recorded attribution, or
// never tested, must read back with them empty rather than invented.
func TestClassicDeviceTestTimesAndAttribution(t *testing.T) {
	t.Parallel()

	var status SourceTestStatus
	body := `{"name":"edge1","testResult":{"error":"AUTHENTICATION_FAILED","startTime":"2026-10-16T00:59:00Z","endTime":"2026-10-16T01:00:00Z","savedAt":"2026-10-16T01:00:01Z"}}`
	if err := json.Unmarshal([]byte(body), &status); err != nil {
		t.Fatal(err)
	}
	if status.TestStartTime != "2026-10-16T00:59:00Z" || status.TestEndTime != "2026-10-16T01:00:00Z" || status.TestSavedAt != "2026-10-16T01:00:01Z" || status.ConnectivityError != "AUTHENTICATION_FAILED" {
		t.Fatalf("decoded %+v", status)
	}
	var untested SourceTestStatus
	if err := json.Unmarshal([]byte(`{"name":"edge2"}`), &untested); err != nil || untested.TestEndTime != "" || untested.TestSavedAt != "" {
		t.Fatalf("an untested device has no test times: %v %+v", err, untested)
	}

}

// Forward unwraps the device's attribution into the object: usernames (empty for
// a deleted user), user ids as decimal strings, ISO times, and updated* mirroring
// created* until the first change. A device created before attribution existed
// has none of them and must read back with them empty rather than invented.
func TestClassicDeviceAttribution(t *testing.T) {
	t.Parallel()

	var device ClassicDevice
	body := `{"name":"edge1","host":"10.0.0.1","createdBy":"craig","createdById":"7","createdAt":"2026-09-01T10:00:00Z","updatedBy":"craig","updatedById":7,"updatedAt":"2026-10-01T10:00:00Z"}`
	if err := json.Unmarshal([]byte(body), &device); err != nil {
		t.Fatal(err)
	}
	if device.CreatedBy != "craig" || device.CreatedByID != "7" || device.CreatedAt != "2026-09-01T10:00:00Z" ||
		device.UpdatedBy != "craig" || device.UpdatedByID != "7" || device.UpdatedAt != "2026-10-01T10:00:00Z" {
		t.Fatalf("decoded %+v", device)
	}
	var bare ClassicDevice
	if err := json.Unmarshal([]byte(`{"name":"old"}`), &bare); err != nil || bare.CreatedAt != "" || bare.CreatedBy != "" || bare.UpdatedByID != "" {
		t.Fatalf("no attribution must read back empty: %v %+v", err, bare)
	}
}
