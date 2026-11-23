package domain

import (
	"errors"
	"fmt"
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

// Canonical returns the key with its fields trimmed, or an error if Forward would refuse or never match it. The prefix is a CIDR
// prefix with no host bits set (Forward does not normalize, so 10.0.0.1/8 is a different key from 10.0.0.0/8 and never matches), or a
// bare address, which Forward reads as a /32 or /128 host route and also writes back that way.
func (k BGPAdvertisementKey) Canonical() (BGPAdvertisementKey, error) {
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
