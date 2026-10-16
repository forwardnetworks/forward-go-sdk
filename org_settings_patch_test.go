package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Wells set collectionRetryDelayMillis to 30000 and perDeviceSnmpConcurrency to
// 1 and wants them back at Forward's defaults. The patch must be able to say
// both things Forward's JsonProp distinguishes: set a value (the retry fields
// merge into one stored config, so touching one leaves the others) and reset it
// with an explicit null, which leaves the property unset (reads back nil)
// instead of storing the default's number (the test server only records the
// request body, so "reads back nil" is Forward's documented storage, not asserted here).
func TestOrgCollectionSettingsPatchSetsAndClears(t *testing.T) {
	t.Parallel()

	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	patches := []OrgCollectionSettingsPatch{
		{CollectionRetryDelayMillis: Ptr(30000), PerDeviceSNMPConcurrency: Ptr(1), CollectionRetries: Ptr(0), CollectionMaxRetryDelayMillis: Ptr(90000)},
		{Clear: []string{"collectionRetryDelayMillis", "perDeviceSnmpConcurrency"}},
		{CommandDelayMS: Ptr(5), Clear: []string{"collectionRetries"}},
	}
	for _, patch := range patches {
		if _, err := client.Collectors.PatchOrganizationSettings(ctx, patch); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		`{"perDeviceSnmpConcurrency":1,"collectionRetries":0,"collectionRetryDelayMillis":30000,"collectionMaxRetryDelayMillis":90000}`,
		`{"collectionRetryDelayMillis":null,"perDeviceSnmpConcurrency":null}`,
		`{"collectionRetries":null,"commandDelayMs":5}`,
	}
	if len(bodies) != len(want) {
		t.Fatalf("bodies = %v", bodies)
	}
	for i := range want {
		if bodies[i] != want[i] {
			t.Errorf("body %d = %s, want %s", i, bodies[i], want[i])
		}
	}

	sent := len(bodies)
	for _, bad := range []OrgCollectionSettingsPatch{
		{Clear: []string{"disabledCommands"}},
		{Clear: []string{"nonsense"}},
		{CollectionRetries: Ptr(1), Clear: []string{"collectionRetries"}},
	} {
		if _, err := client.Collectors.PatchOrganizationSettings(ctx, bad); err == nil {
			t.Errorf("%+v must be refused", bad)
		}
	}
	if len(bodies) != sent {
		t.Fatal("a refused patch must not reach Forward")
	}
}
