package domain

import (
	"errors"
	"strings"
)

// ResolveNetworkID is the one rule for which network a network-scoped call addresses: an explicit ID wins once trimmed of
// space, an empty one falls back to the network the client is bound to (ForNetwork), and with neither the call is refused
// before a request is made. The HTTP adapter and the use cases both reach it, so the rule and its error are never copied.
func ResolveNetworkID(explicit, bound string) (string, error) {
	if id := strings.TrimSpace(explicit); id != "" {
		return id, nil
	}
	if bound == "" {
		return "", errors.New("forward: network ID is required (bind one with ForNetwork or pass it explicitly)")
	}
	return bound, nil
}
