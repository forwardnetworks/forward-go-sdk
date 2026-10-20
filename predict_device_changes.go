package forward

import (
	"encoding/json"
	"fmt"
	"sort"
)

// DeviceChanges is what a change set records for one device, as DeviceChanges serializes it: staged CLI commands, whether a whole-config
// replacement is stored, whether firewall rule changes are pending, and the BGP advertisements injected or modified and withdrawn. An
// entry can also be empty (restoring a device's only staged advertisement leaves one), which is why Edited exists.
type DeviceChanges struct {
	// Commands are the staged CLI commands, newline separated; empty when none are staged (or a config file is stored instead).
	Commands string `json:"commands,omitempty"`
	// HasConfig says a whole-config replacement is stored (Predict.SaveDeviceConfig); read it with GetDraftConfig or GetCommittedConfig.
	HasConfig bool `json:"hasConfig"`
	// HasSecurityRuleChanges says firewall rule changes are pending. The rule changes themselves are not in this object (Forward marks
	// them @JsonIgnore); Predict.SecurityRulesDiff reads them per device.
	HasSecurityRuleChanges bool `json:"hasSecurityRuleChanges"`
	// AddedAdvertisements are the received BGP advertisements injected or modified on this device. Forward does not send Device; Changes fills it from the map key.
	AddedAdvertisements []BGPAdvertisement `json:"addedAdvertisements,omitempty"`
	// RemovedAdvertisements are the received BGP advertisements withdrawn on this device, by key.
	RemovedAdvertisements []BGPAdvertisementKey `json:"removedAdvertisements,omitempty"`
}

// Edited says the device has any recorded change at all.
func (c DeviceChanges) Edited() bool {
	return c.Commands != "" || c.HasConfig || c.HasSecurityRuleChanges || len(c.AddedAdvertisements) > 0 || len(c.RemovedAdvertisements) > 0
}

func decodeDeviceChanges(raw map[string]json.RawMessage) (map[string]DeviceChanges, error) {
	out := make(map[string]DeviceChanges, len(raw))
	for device, value := range raw {
		var changes DeviceChanges
		if err := json.Unmarshal(value, &changes); err != nil {
			return nil, fmt.Errorf("forward: changes of device %s: %w", device, err)
		}
		for i := range changes.AddedAdvertisements {
			changes.AddedAdvertisements[i].Device = device
		}
		out[device] = changes
	}
	return out, nil
}

// Changes decodes DeviceToChanges into typed per-device changes (keys are Forward's lowercased device names). It is how a caller confirms
// a withdraw, an inject, staged commands or pending firewall rule changes were recorded: a change set that holds only BGP advertisements
// has devices with no Commands but with AddedAdvertisements or RemovedAdvertisements, so count a device as edited with Edited(), not by
// Commands alone. The change-set list never includes DeviceToChanges (only a modified-device count); read a version (Predict.GetHeadCommit, GetCommit) or the draft
// (Predict.GetDraft) for the content.
func (c ChangeSet) Changes() (map[string]DeviceChanges, error) {
	return decodeDeviceChanges(c.DeviceToChanges)
}

// Changes decodes this version's per-device changes; see ChangeSet.Changes.
func (c ChangeSetCommit) Changes() (map[string]DeviceChanges, error) {
	return decodeDeviceChanges(c.DeviceToChanges)
}

// EditedDevices lists, sorted, the devices that have any recorded change.
func EditedDevices(changes map[string]DeviceChanges) []string {
	var out []string
	for device, c := range changes {
		if c.Edited() {
			out = append(out, device)
		}
	}
	sort.Strings(out)
	return out
}
