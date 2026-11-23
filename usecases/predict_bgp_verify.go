package usecases

import (
	"context"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/netip"
	"strings"
)

// VerifyBGPStaging reads the draft's BGP diffs (BGPAdvertisementDiffs) and matches every staged advertisement and withdrawn key to its
// row. It returns the report together with an error wrapping ErrBGPKeyMatchedNothing (the key named) when a withdrawal is ABSENT or
// any key has no row. It does not call it an error when a staged advertisement reads INJECTED where the caller meant to replace a
// received route: that is how an EBGP/IBGP type mismatch shows (the received route stays and the staged one is added beside it),
// so a caller that expects MODIFIED checks Outcome.Change itself. DUPLICATED is reported as is. Preview: the diffs shape is from
// Forward's source (BgpAdvertisementDiffInfo), not yet recorded live.
func (s *Predict) VerifyBGPStaging(ctx context.Context, networkID, changeSetID, deviceName string, staged []domain.BGPAdvertisement, withdrawn []domain.BGPAdvertisementKey) (*domain.BGPStagingReport, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	diffs, resp, err := s.BGPAdvertisementDiffs(ctx, networkID, changeSetID, deviceName)
	if err != nil {
		return nil, resp, err
	}
	// Forward keeps staged and withdrawn advertisements as two sets (computeBgpAdvertisementDiff) and a key can be in both, so a staged
	// key is matched only against added rows and a withdrawal only against withdrawn rows: one map would let a withdrawn row answer
	// for a staged key, or an added row for a withdrawal.
	added := map[domain.BGPAdvertisementKey]*domain.BGPAdvertisementDiff{}
	removed := map[domain.BGPAdvertisementKey]*domain.BGPAdvertisementDiff{}
	for i := range diffs {
		d := &diffs[i]
		ad, err := d.AddedAdvertisement()
		if err != nil {
			return nil, resp, err
		}
		if ad != nil {
			added[diffKeyForm(domain.BGPAdvertisementKey{VRF: ad.VRF, ExternalPeer: ad.ExternalPeer, Prefix: ad.Prefix})] = d
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
	report := &domain.BGPStagingReport{}
	var missed []string
	look := func(key domain.BGPAdvertisementKey, withdrawal bool) domain.BGPKeyOutcome {
		out := domain.BGPKeyOutcome{Key: key}
		rows := added
		if withdrawal {
			rows = removed
		}
		if d := rows[diffKeyForm(key)]; d != nil {
			out.Found, out.Change, out.Diff = true, d.Kind(), d
		}
		if !out.Found || (withdrawal && out.Change == domain.BGPChangeAbsent) {
			missed = append(missed, fmt.Sprintf("%s %s via %s in VRF %s", map[bool]string{true: "withdraw", false: "stage"}[withdrawal], key.Prefix, key.ExternalPeer, key.VRF))
		}
		return out
	}
	for _, ad := range staged {
		report.Staged = append(report.Staged, look(domain.BGPAdvertisementKey{VRF: ad.VRF, ExternalPeer: ad.ExternalPeer, Prefix: ad.Prefix}, false))
	}
	for _, key := range withdrawn {
		report.Withdrawn = append(report.Withdrawn, look(key, true))
	}
	if len(missed) != 0 {
		return report, resp, fmt.Errorf("%w: %s", domain.ErrBGPKeyMatchedNothing, strings.Join(missed, "; "))
	}
	return report, resp, nil
}

// diffKeyForm makes a key comparable: trimmed, and a bare prefix address written as the host route Forward writes back.
func diffKeyForm(k domain.BGPAdvertisementKey) domain.BGPAdvertisementKey {
	k = domain.BGPAdvertisementKey{VRF: strings.TrimSpace(k.VRF), ExternalPeer: strings.TrimSpace(k.ExternalPeer), Prefix: strings.TrimSpace(k.Prefix)}
	if addr, err := netip.ParseAddr(k.Prefix); err == nil {
		k.Prefix = netip.PrefixFrom(addr, addr.BitLen()).String()
	}
	if addr, err := netip.ParseAddr(k.ExternalPeer); err == nil {
		k.ExternalPeer = addr.String()
	}
	return k
}
