package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
)

// BGPAdvertisementKey identifies a received BGP advertisement on a device: the VRF it arrived in, the external peer it came from
// and the prefix. It is the whole body of a withdraw or restore.
type BGPAdvertisementKey struct {
	VRF          string `json:"vrf"`
	ExternalPeer string `json:"externalPeer"`
	Prefix       string `json:"prefix"`
}

// canonical returns the key with its fields trimmed, or an error if Forward would refuse or never match it. The prefix is a CIDR
// prefix with no host bits set (Forward does not normalize, so 10.0.0.1/8 is a different key from 10.0.0.0/8 and never matches), or a
// bare address, which Forward reads as a /32 or /128 host route and also writes back that way.
func (k BGPAdvertisementKey) canonical() (BGPAdvertisementKey, error) {
	k = BGPAdvertisementKey{VRF: strings.TrimSpace(k.VRF), ExternalPeer: strings.TrimSpace(k.ExternalPeer), Prefix: strings.TrimSpace(k.Prefix)}
	if k.VRF == "" {
		return k, errors.New("forward: BGP advertisement VRF is required")
	}
	if _, err := netip.ParseAddr(k.ExternalPeer); err != nil {
		return k, fmt.Errorf("forward: BGP advertisement external peer %q is not an IP address", k.ExternalPeer)
	}
	if strings.Contains(k.Prefix, "/") {
		prefix, err := netip.ParsePrefix(k.Prefix)
		if err != nil {
			return k, fmt.Errorf("forward: BGP advertisement prefix %q is not a CIDR prefix", k.Prefix)
		}
		if prefix.Masked() != prefix {
			return k, fmt.Errorf("forward: BGP advertisement prefix %q has host bits set; use %q", k.Prefix, prefix.Masked())
		}
	} else if _, err := netip.ParseAddr(k.Prefix); err != nil {
		return k, fmt.Errorf("forward: BGP advertisement prefix %q is not a CIDR prefix or an address", k.Prefix)
	}
	return k, nil
}

// WithdrawBGPAdvertisement stages the withdrawal of a received BGP advertisement on a device (POST
// .../draft/devices/{device}/bgp-advertisements?action=remove, body the key; ChangeSetController.withdrawAdvertisement): the
// "withdraw" of Forward's "modify received BGP advertisements", for failure and de-peering what-ifs. For a peer OUTSIDE the
// snapshot (an ISP, say), shutting the neighbor with a CLI command removes the session from the predicted snapshot but still
// seeds the routes learned from it (BgpDeviceSeedRoutesDiscoverer.applyInboundPolicy), so model that peer loss by withdrawing
// its advertisements. A peer that is itself a collected device needs no withdrawal: its routes are never seeded once the
// session is gone. Forward does not check at staging time that the key matches a received advertisement; that is decided when
// the change set runs, and a key matching nothing is ignored there. It cancels a staged advertisement with the same key. The
// device needs received-route data: collectBgpAdvertisements on, with the BGP table type BOTH or ADJ_RIB_IN covering the peer.
// The key is trimmed before it is sent; its prefix must have no host bits set, and a bare address is a host route.
func (s *PredictService) WithdrawBGPAdvertisement(ctx context.Context, networkID, changeSetID, deviceName string, key BGPAdvertisementKey) (*Response, error) {
	key, err := key.canonical()
	if err != nil {
		return nil, err
	}
	return s.bgpAction(ctx, networkID, changeSetID, deviceName, "remove", key, "Predict.WithdrawBGPAdvertisement")
}

// WithdrawBGPAdvertisements is WithdrawBGPAdvertisement for several keys on one device (?action=bulkRemove; ChangeSetController.withdrawAdvertisements). A repeated key is refused.
func (s *PredictService) WithdrawBGPAdvertisements(ctx context.Context, networkID, changeSetID, deviceName string, keys []BGPAdvertisementKey) (*Response, error) {
	keys, err := checkBGPKeys(keys)
	if err != nil {
		return nil, err
	}
	return s.bgpAction(ctx, networkID, changeSetID, deviceName, "bulkRemove", keys, "Predict.WithdrawBGPAdvertisements")
}

// RestoreBGPAdvertisement drops whatever is staged for the key on the device, an injected or modified advertisement or a
// withdrawal, so the received advertisement stands as collected (?action=restore, body the key; 204;
// ChangeSetController.restoreAdvertisement). A key with nothing staged is ignored. It works on the draft or, when there is none,
// on the latest commit (it then creates a draft that undoes the committed edit for that key).
func (s *PredictService) RestoreBGPAdvertisement(ctx context.Context, networkID, changeSetID, deviceName string, key BGPAdvertisementKey) (*Response, error) {
	key, err := key.canonical()
	if err != nil {
		return nil, err
	}
	return s.bgpAction(ctx, networkID, changeSetID, deviceName, "restore", key, "Predict.RestoreBGPAdvertisement")
}

// RestoreBGPAdvertisements is RestoreBGPAdvertisement for several keys on one device (?action=bulkRestore; ChangeSetController.restoreAdvertisements). A repeated key is refused.
func (s *PredictService) RestoreBGPAdvertisements(ctx context.Context, networkID, changeSetID, deviceName string, keys []BGPAdvertisementKey) (*Response, error) {
	keys, err := checkBGPKeys(keys)
	if err != nil {
		return nil, err
	}
	return s.bgpAction(ctx, networkID, changeSetID, deviceName, "bulkRestore", keys, "Predict.RestoreBGPAdvertisements")
}

// checkBGPKeys canonicalizes every key and refuses an empty list or a repeated key: Forward stores a repeated key from one bulk body
// as it is and then fails when the change set runs (its seed-route map has no merge for equal keys), so the SDK refuses it first.
func checkBGPKeys(keys []BGPAdvertisementKey) ([]BGPAdvertisementKey, error) {
	if len(keys) == 0 {
		return nil, errors.New("forward: at least one BGP advertisement key is required")
	}
	seen := map[BGPAdvertisementKey]bool{}
	out := make([]BGPAdvertisementKey, 0, len(keys))
	for _, key := range keys {
		key, err := key.canonical()
		if err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, fmt.Errorf("forward: BGP advertisement key %s %s %s is given twice", key.VRF, key.ExternalPeer, key.Prefix)
		}
		seen[key] = true
		out = append(out, key)
	}
	return out, nil
}

func (s *PredictService) bgpAction(ctx context.Context, networkID, changeSetID, deviceName, action string, body any, operation string) (*Response, error) {
	if err := s.client.requireCapability(CapabilityStructuredBGPAdvertisements); err != nil {
		return nil, err
	}
	path, err := s.csDevicePath(networkID, changeSetID, deviceName)
	if err != nil {
		return nil, err
	}
	resp, err := s.doJSON(ctx, http.MethodPost, path+"/bgp-advertisements?action="+action, body, nil, operation)
	if err == nil {
		s.client.observeCapability(CapabilityStructuredBGPAdvertisements)
	}
	return resp, err
}
