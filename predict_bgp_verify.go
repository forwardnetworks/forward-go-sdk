package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
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

// VerifyBGPStaging reads the draft's BGP diffs (BGPAdvertisementDiffs) and matches every staged advertisement and withdrawn key to its
// row. It returns the report together with an error wrapping ErrBGPKeyMatchedNothing (the key named) when a withdrawal is ABSENT or
// any key has no row. It does not call it an error when a staged advertisement reads INJECTED where the caller meant to replace a
// received route: that is how an EBGP/IBGP type mismatch shows (the received route stays and the staged one is added beside it),
// so a caller that expects MODIFIED checks Outcome.Change itself. DUPLICATED is reported as is. Preview: the diffs shape is from
// Forward's source (BgpAdvertisementDiffInfo), not yet recorded live.
func (s *PredictService) VerifyBGPStaging(ctx context.Context, networkID, changeSetID, deviceName string, staged []BGPAdvertisement, withdrawn []BGPAdvertisementKey) (*BGPStagingReport, *Response, error) {
	diffs, resp, err := s.BGPAdvertisementDiffs(ctx, networkID, changeSetID, deviceName)
	if err != nil {
		return nil, resp, err
	}
	// Forward keeps staged and withdrawn advertisements as two sets (computeBgpAdvertisementDiff) and a key can be in both, so a staged
	// key is matched only against added rows and a withdrawal only against withdrawn rows: one map would let a withdrawn row answer
	// for a staged key, or an added row for a withdrawal.
	added := map[BGPAdvertisementKey]*BGPAdvertisementDiff{}
	removed := map[BGPAdvertisementKey]*BGPAdvertisementDiff{}
	for i := range diffs {
		d := &diffs[i]
		ad, err := d.AddedAdvertisement()
		if err != nil {
			return nil, resp, err
		}
		if ad != nil {
			added[diffKeyForm(BGPAdvertisementKey{VRF: ad.VRF, ExternalPeer: ad.ExternalPeer, Prefix: ad.Prefix})] = d
			continue
		}
		key, err := d.RemovedKey()
		if err != nil {
			return nil, resp, err
		}
		if key != nil {
			removed[diffKeyForm(*key)] = d
		}
	}
	report := &BGPStagingReport{}
	var missed []string
	look := func(key BGPAdvertisementKey, withdrawal bool) BGPKeyOutcome {
		out := BGPKeyOutcome{Key: key}
		rows := added
		if withdrawal {
			rows = removed
		}
		if d := rows[diffKeyForm(key)]; d != nil {
			out.Found, out.Change, out.Diff = true, d.Kind(), d
		}
		if !out.Found || (withdrawal && out.Change == BGPChangeAbsent) {
			missed = append(missed, fmt.Sprintf("%s %s via %s in VRF %s", map[bool]string{true: "withdraw", false: "stage"}[withdrawal], key.Prefix, key.ExternalPeer, key.VRF))
		}
		return out
	}
	for _, ad := range staged {
		report.Staged = append(report.Staged, look(BGPAdvertisementKey{VRF: ad.VRF, ExternalPeer: ad.ExternalPeer, Prefix: ad.Prefix}, false))
	}
	for _, key := range withdrawn {
		report.Withdrawn = append(report.Withdrawn, look(key, true))
	}
	if len(missed) != 0 {
		return report, resp, fmt.Errorf("%w: %s", ErrBGPKeyMatchedNothing, strings.Join(missed, "; "))
	}
	return report, resp, nil
}

// diffKeyForm makes a key comparable: trimmed, and a bare prefix address written as the host route Forward writes back.
func diffKeyForm(k BGPAdvertisementKey) BGPAdvertisementKey {
	k = BGPAdvertisementKey{VRF: strings.TrimSpace(k.VRF), ExternalPeer: strings.TrimSpace(k.ExternalPeer), Prefix: strings.TrimSpace(k.Prefix)}
	if addr, err := netip.ParseAddr(k.Prefix); err == nil {
		k.Prefix = netip.PrefixFrom(addr, addr.BitLen()).String()
	}
	if addr, err := netip.ParseAddr(k.ExternalPeer); err == nil {
		k.ExternalPeer = addr.String()
	}
	return k
}
