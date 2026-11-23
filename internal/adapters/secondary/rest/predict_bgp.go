package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
)

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
func (s *Predict) WithdrawBGPAdvertisement(ctx context.Context, networkID, changeSetID, deviceName string, key ports.BGPAdvertisementKey) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	key, err := key.Canonical()
	if err != nil {
		return nil, err
	}
	return s.bgpAction(ctx, networkID, changeSetID, deviceName, "remove", key, "Predict.WithdrawBGPAdvertisement")
}

// WithdrawBGPAdvertisements is WithdrawBGPAdvertisement for several keys on one device (?action=bulkRemove; ChangeSetController.withdrawAdvertisements). A repeated key is refused.
func (s *Predict) WithdrawBGPAdvertisements(ctx context.Context, networkID, changeSetID, deviceName string, keys []ports.BGPAdvertisementKey) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *Predict) RestoreBGPAdvertisement(ctx context.Context, networkID, changeSetID, deviceName string, key ports.BGPAdvertisementKey) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	key, err := key.Canonical()
	if err != nil {
		return nil, err
	}
	return s.bgpAction(ctx, networkID, changeSetID, deviceName, "restore", key, "Predict.RestoreBGPAdvertisement")
}

// RestoreBGPAdvertisements is RestoreBGPAdvertisement for several keys on one device (?action=bulkRestore; ChangeSetController.restoreAdvertisements). A repeated key is refused.
func (s *Predict) RestoreBGPAdvertisements(ctx context.Context, networkID, changeSetID, deviceName string, keys []ports.BGPAdvertisementKey) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	keys, err := checkBGPKeys(keys)
	if err != nil {
		return nil, err
	}
	return s.bgpAction(ctx, networkID, changeSetID, deviceName, "bulkRestore", keys, "Predict.RestoreBGPAdvertisements")
}

// checkBGPKeys canonicalizes every key and refuses an empty list or a repeated key: Forward stores a repeated key from one bulk body
// as it is and then fails when the change set runs (its seed-route map has no merge for equal keys), so the SDK refuses it first.
func checkBGPKeys(keys []ports.BGPAdvertisementKey) ([]ports.BGPAdvertisementKey, error) {
	if len(keys) == 0 {
		return nil, errors.New("forward: at least one BGP advertisement key is required")
	}
	seen := map[ports.BGPAdvertisementKey]bool{}
	out := make([]ports.BGPAdvertisementKey, 0, len(keys))
	for _, key := range keys {
		key, err := key.Canonical()
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

func (s *Predict) bgpAction(ctx context.Context, networkID, changeSetID, deviceName, action string, body any, operation string) (*ports.Response, error) {
	if err := s.client.requireCapability(ports.CapabilityStructuredBGPAdvertisements); err != nil {
		return nil, err
	}
	path, err := s.csDevicePath(networkID, changeSetID, deviceName)
	if err != nil {
		return nil, err
	}
	resp, err := s.doJSON(ctx, http.MethodPost, path+"/bgp-advertisements?action="+action, body, nil, operation)
	if err == nil {
		s.client.observeCapability(ports.CapabilityStructuredBGPAdvertisements)
	}
	return resp, err
}
