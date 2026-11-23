package domain

import (
	"encoding/json"
	"errors"
	"fmt"
)

// BGPAdvertisementChange is BGPAdvertisementDiff.Change as Forward's BgpChangeType names it.
type BGPAdvertisementChange string

const (
	// BGPChangeInjected is a staged advertisement whose key the base snapshot does not have.
	BGPChangeInjected BGPAdvertisementChange = "INJECTED"
	// BGPChangeModified is a staged advertisement with the key of a received one but different attributes.
	BGPChangeModified BGPAdvertisementChange = "MODIFIED"
	// BGPChangeDuplicated is a staged advertisement identical to the received one with its key: it changes nothing.
	BGPChangeDuplicated BGPAdvertisementChange = "DUPLICATED"
	// BGPChangeWithdrawn is a withdrawal that removed a received advertisement.
	BGPChangeWithdrawn BGPAdvertisementChange = "WITHDRAWN"
	// BGPChangeAbsent is a withdrawal of a key the base snapshot does not have: Forward ignores it.
	BGPChangeAbsent BGPAdvertisementChange = "ABSENT"
)

// Kind returns Change as a typed value. An unknown string (a newer Forward) comes back as itself.
func (d BGPAdvertisementDiff) Kind() BGPAdvertisementChange { return BGPAdvertisementChange(d.Change) }

// OriginalAdvertisement decodes Original, the base snapshot's advertisement with the same key; nil when Forward sent none.
func (d BGPAdvertisementDiff) OriginalAdvertisement() (*BGPAdvertisement, error) {
	return decodeBGPAdvertisement(d.Original)
}

// AddedAdvertisement decodes UpdateAdded, the staged advertisement; nil for a withdrawal.
func (d BGPAdvertisementDiff) AddedAdvertisement() (*BGPAdvertisement, error) {
	return decodeBGPAdvertisement(d.UpdateAdded)
}

// RemovedKey decodes UpdateRemoved, the key a withdrawal named; nil for an injection.
func (d BGPAdvertisementDiff) RemovedKey() (*BGPAdvertisementKey, error) {
	if len(d.UpdateRemoved) == 0 || string(d.UpdateRemoved) == "null" {
		return nil, nil
	}
	var key BGPAdvertisementKey
	if err := json.Unmarshal(d.UpdateRemoved, &key); err != nil {
		return nil, fmt.Errorf("forward: decode withdrawn BGP key: %w", err)
	}
	return &key, nil
}

func decodeBGPAdvertisement(raw json.RawMessage) (*BGPAdvertisement, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var ad BGPAdvertisement
	if err := json.Unmarshal(raw, &ad); err != nil {
		return nil, fmt.Errorf("forward: decode BGP advertisement: %w", err)
	}
	return &ad, nil
}

// ErrBGPKeyMatchedNothing is VerifyBGPStaging's error when a withdrawal named a key the base snapshot has no advertisement for
// (Change ABSENT), or a staged key came back in no diff at all. Forward does not refuse such a stage; it ignores it when the change
// set runs, so without this check a withdraw that matched nothing looks like a withdraw that worked.
var ErrBGPKeyMatchedNothing = errors.New("forward: a staged BGP key matched nothing")

// BGPKeyOutcome is what the diffs view says about one key the caller staged or withdrew.
type BGPKeyOutcome struct {
	Key BGPAdvertisementKey
	// Found is false when no diff row carries the key: the stage is not in the draft, or sits in another device's draft.
	Found  bool
	Change BGPAdvertisementChange
	Diff   *BGPAdvertisementDiff
}

// BGPStagingReport is VerifyBGPStaging's read-back, one outcome per key in the order given.
type BGPStagingReport struct {
	Staged    []BGPKeyOutcome
	Withdrawn []BGPKeyOutcome
}
