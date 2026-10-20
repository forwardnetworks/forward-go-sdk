package forward

import (
	"encoding/json"
	"reflect"
	"testing"
)

// verify-change showed "edits 0 device(s)" for a change set holding only BGP advertisements because the count looked at commands. Forward
// serializes DeviceChanges as {commands?, hasConfig, hasSecurityRuleChanges, addedAdvertisements?, removedAdvertisements?}; a device is in
// deviceToChanges when it has any of them. The typed read must expose all five so a withdraw or inject can be confirmed as recorded.
func TestChangeSetChangesExposeAdvertisementsCommandsAndFlags(t *testing.T) {
	t.Parallel()

	body := `{"id":"cs1","name":"depeer","networkId":"205507","snapshotId":"9001","deviceToChanges":{
		"ldc31-rtedg-1":{"hasConfig":false,"hasSecurityRuleChanges":false,
			"removedAdvertisements":[{"externalPeer":"4.16.225.165","prefix":"0.0.0.0/0","vrf":"default"}]},
		"edge2":{"hasConfig":false,"hasSecurityRuleChanges":false,
			"addedAdvertisements":[{"externalPeer":"192.0.2.1","prefix":"10.0.0.0/8","vrf":"default","type":"EBGP","nextHop":"192.0.2.1","origin":"IGP","asPath":[65001],"med":5,"communities":["65000:1"]}]},
		"core1":{"commands":"interface Gi1\nshutdown","hasConfig":false,"hasSecurityRuleChanges":false},
		"fw1":{"hasConfig":false,"hasSecurityRuleChanges":true},
		"cfg1":{"hasConfig":true,"hasSecurityRuleChanges":false},
		"idle":{"hasConfig":false,"hasSecurityRuleChanges":false}}}`
	var changeSet ChangeSet
	if err := json.Unmarshal([]byte(body), &changeSet); err != nil {
		t.Fatal(err)
	}
	changes, err := changeSet.Changes()
	if err != nil {
		t.Fatal(err)
	}
	withdraw := changes["ldc31-rtedg-1"]
	if withdraw.Commands != "" || !reflect.DeepEqual(withdraw.RemovedAdvertisements, []BGPAdvertisementKey{{VRF: "default", ExternalPeer: "4.16.225.165", Prefix: "0.0.0.0/0"}}) || !withdraw.Edited() {
		t.Fatalf("a withdraw-only device is edited and shows the key: %+v", withdraw)
	}
	inject := changes["edge2"]
	if len(inject.AddedAdvertisements) != 1 || inject.AddedAdvertisements[0].Device != "edge2" || inject.AddedAdvertisements[0].Prefix != "10.0.0.0/8" ||
		inject.AddedAdvertisements[0].MED == nil || *inject.AddedAdvertisements[0].MED != 5 || inject.AddedAdvertisements[0].Type != "EBGP" {
		t.Fatalf("an inject keeps its attributes and the device it is on: %+v", inject)
	}
	if !changes["core1"].Edited() || changes["core1"].Commands != "interface Gi1\nshutdown" || !changes["fw1"].HasSecurityRuleChanges || !changes["cfg1"].HasConfig {
		t.Fatalf("commands, firewall and config flags: %+v", changes)
	}
	if changes["idle"].Edited() {
		t.Error("a device with nothing recorded is not edited")
	}
	if got, want := EditedDevices(changes), []string{"cfg1", "core1", "edge2", "fw1", "ldc31-rtedg-1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("EditedDevices = %v, want %v", got, want)
	}

	// The same typed read works on a stored version and on the draft, which embed the change set's fields.
	var commit ChangeSetCommit
	if err := json.Unmarshal([]byte(body), &commit); err != nil {
		t.Fatal(err)
	}
	if fromCommit, err := commit.Changes(); err != nil || len(EditedDevices(fromCommit)) != 5 {
		t.Fatalf("commit: %v %v", err, fromCommit)
	}
	var draft ChangeSetDraft
	if err := json.Unmarshal([]byte(`{"draft":`+body+`}`), &draft); err != nil || draft.Draft == nil {
		t.Fatalf("draft: %v", err)
	}
	if fromDraft, err := draft.Draft.Changes(); err != nil || len(EditedDevices(fromDraft)) != 5 {
		t.Fatalf("draft changes: %v %v", err, fromDraft)
	}
	var empty ChangeSet
	if none, err := empty.Changes(); err != nil || len(none) != 0 {
		t.Fatalf("a change set with no device changes decodes to none: %v %v", err, none)
	}
	bad := ChangeSet{DeviceToChanges: map[string]json.RawMessage{"x": json.RawMessage(`{"hasConfig":"yes"}`)}}
	if _, err := bad.Changes(); err == nil {
		t.Error("a malformed device entry is an error naming the device")
	}
}
