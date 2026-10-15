package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func capturedBody(t *testing.T, respond string) (*httptest.Server, *[]byte) {
	t.Helper()
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, respond)
	}))
	t.Cleanup(server.Close)
	return server, &body
}

func decodeBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("body %q: %v", raw, err)
	}
	return out
}

// An enable password lives in a PRIVILEGED_MODE credential named by
// cliCredential2Id, so every write path must be able to carry it, and the
// per-device collection options must reach the wire only when stated, so an
// unset option leaves Forward's default (false is a different statement).
func TestClassicDeviceWritesCarryCredential2And3AndCollectionOptions(t *testing.T) {
	ctx := context.Background()
	server, body := capturedBody(t, `{"name":"sw1","host":"10.0.0.1"}`)
	client := newTestClient(t, server.URL)

	if _, _, err := client.ClassicDevices.Create(ctx, "net-1", ClassicDeviceRequest{
		Name: "sw1", Host: "10.0.0.1", CLICredentialID: "L-1", CLICredential2ID: "PM-2", CLICredential3ID: "EM-3",
		CollectBGPAdvertisements: Ptr(true), BGPTableType: "ADJ_RIB_IN", BGPPeerType: "EBGP", BGPAndBMPFailureHandling: "SWITCH_TO_CLI",
		DisableIPv6Collection: Ptr(false), FullCollectionLog: Ptr(true), EnableSNMPCollection: Ptr(false),
	}); err != nil {
		t.Fatal(err)
	}
	got := decodeBody(t, *body)
	for key, want := range map[string]any{
		"cliCredential2Id": "PM-2", "cliCredential3Id": "EM-3", "collectBgpAdvertisements": true, "bgpTableType": "ADJ_RIB_IN",
		"bgpPeerType": "EBGP", "bgpAndBmpFailureHandling": "SWITCH_TO_CLI", "disableIpv6Collection": false,
		"fullCollectionLog": true, "enableSnmpCollection": false,
	} {
		if got[key] != want {
			t.Errorf("create %s = %v, want %v (body %s)", key, got[key], want, *body)
		}
	}
	if _, _, err := client.ClassicDevices.Create(ctx, "net-1", ClassicDeviceRequest{Name: "sw2", Host: "10.0.0.2"}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"collectBgpAdvertisements", "bgpTableType", "disableIpv6Collection", "fullCollectionLog", "enableSnmpCollection", "cliCredential2Id"} {
		if _, sent := decodeBody(t, *body)[key]; sent {
			t.Errorf("an unset %s must not be sent: %s", key, *body)
		}
	}

	if _, err := client.ClassicDevices.PutBatch(ctx, "net-1", []ClassicDeviceBatchItem{{
		Name: "sw1", Host: "10.0.0.1", CLICredentialID: "L-1", CLICredential2ID: "PM-2", CLICredential3ID: "EM-3",
		BGPAndBMPFailureHandling: "ALWAYS_USE_CLI", DisableIPv6Collection: Ptr(true), FullCollectionLog: Ptr(false),
	}}); err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := json.Unmarshal(*body, &items); err != nil || len(items) != 1 {
		t.Fatalf("batch body %s: %v", *body, err)
	}
	for key, want := range map[string]any{
		"cliCredential2Id": "PM-2", "cliCredential3Id": "EM-3", "bgpAndBmpFailureHandling": "ALWAYS_USE_CLI",
		"disableIpv6Collection": true, "fullCollectionLog": false,
	} {
		if items[0][key] != want {
			t.Errorf("batch %s = %v, want %v (body %s)", key, items[0][key], want, *body)
		}
	}
}

// PATCH carries the new properties, and an explicit JSON null is the only way
// to clear one (Forward's JsonProp: absent = leave, null = clear), which a
// pointer cannot express. A property that is both set and cleared, or one that
// cannot be cleared, is refused before any request.
func TestClassicDevicePatchSetsAndClearsProperties(t *testing.T) {
	ctx := context.Background()
	server, body := capturedBody(t, `{"name":"sw1","host":"10.0.0.1"}`)
	client := newTestClient(t, server.URL)

	if _, _, err := client.ClassicDevices.Patch(ctx, "net-1", "sw1", ClassicDevicePatch{
		CLICredential2ID: Ptr("PM-2"), CollectBGPAdvertisements: Ptr(false), BGPTableType: Ptr("BOTH"),
		Clear: []string{"cliCredential3Id", "collectorId"},
	}); err != nil {
		t.Fatal(err)
	}
	got := decodeBody(t, *body)
	if got["cliCredential2Id"] != "PM-2" || got["collectBgpAdvertisements"] != false || got["bgpTableType"] != "BOTH" {
		t.Errorf("set properties missing: %s", *body)
	}
	for _, key := range []string{"cliCredential3Id", "collectorId"} {
		if value, sent := got[key]; !sent || value != nil {
			t.Errorf("%s must be an explicit null: %s", key, *body)
		}
	}
	if _, sent := got["note"]; sent {
		t.Errorf("an unmentioned property must be absent: %s", *body)
	}

	*body = nil
	for name, patch := range map[string]ClassicDevicePatch{
		"set and cleared": {CLICredential2ID: Ptr("PM-2"), Clear: []string{"cliCredential2Id"}},
		"not clearable":   {Clear: []string{"host"}},
	} {
		if _, _, err := client.ClassicDevices.Patch(ctx, "net-1", "sw1", patch); err == nil || len(*body) != 0 {
			t.Errorf("%s must fail before any request (err=%v, body=%q)", name, err, *body)
		}
	}
}

func TestClassicDeviceReadsCredential2And3AndCollectionOptions(t *testing.T) {
	var device ClassicDevice
	if err := json.Unmarshal([]byte(`{"name":"sw1","host":"h","cliCredential2Id":"PM-2","cliCredential3Id":"EM-3","jumpServerId":"J-0","collectBgpAdvertisements":true,"bgpTableType":"ADJ_RIB_OUT","bgpPeerType":"IBGP","bgpAndBmpFailureHandling":"ALWAYS_USE_BMP","disableIpv6Collection":false,"fullCollectionLog":true}`), &device); err != nil {
		t.Fatal(err)
	}
	if device.CLICredential2ID != "PM-2" || device.CLICredential3ID != "EM-3" || device.JumpServerID != "J-0" ||
		device.CollectBGPAdvertisements == nil || !*device.CollectBGPAdvertisements || device.BGPTableType != "ADJ_RIB_OUT" ||
		device.BGPPeerType != "IBGP" || device.BGPAndBMPFailureHandling != "ALWAYS_USE_BMP" ||
		device.DisableIPv6Collection == nil || *device.DisableIPv6Collection || device.FullCollectionLog == nil || !*device.FullCollectionLog {
		t.Fatalf("device = %+v", device)
	}
	var bare ClassicDevice
	if err := json.Unmarshal([]byte(`{"name":"sw2","host":"h"}`), &bare); err != nil || bare.DisableIPv6Collection != nil || bare.CollectBGPAdvertisements != nil {
		t.Fatalf("unstated options must stay nil: %+v, %v", bare, err)
	}
}
