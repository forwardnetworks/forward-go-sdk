package rest

import (
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"sort"
	"sync"
)

// CapabilitiesService exposes the configured profile plus runtime evidence.
type Capabilities service

type capabilityRegistry struct {
	mu      sync.RWMutex
	profile ports.CapabilityProfile
	status  map[ports.Capability]ports.CapabilityStatus
}

func newCapabilityRegistry(profile ports.CapabilityProfile) *capabilityRegistry {
	profile.Features = cloneCapabilityFeatures(profile.Features)
	r := &capabilityRegistry{profile: profile, status: make(map[ports.Capability]ports.CapabilityStatus)}
	for capability, support := range profile.Features {
		if support == "" {
			support = ports.CapabilityUnknown
		}
		r.status[capability] = ports.CapabilityStatus{Capability: capability, Support: support, Source: "configured profile"}
	}
	return r
}

// Profile returns a copy of the selected deployment profile.
func (s *Capabilities) Profile() ports.CapabilityProfile {
	if s == nil || s.client == nil || s.client.capabilities == nil {
		return ports.CapabilityProfile{}
	}
	r := s.client.capabilities
	r.mu.RLock()
	defer r.mu.RUnlock()
	profile := r.profile
	profile.Features = cloneCapabilityFeatures(profile.Features)
	return profile
}

// Support reports whether capability is supported, unsupported, or unknown.
func (s *Capabilities) Support(capability ports.Capability) ports.CapabilityStatus {
	if s == nil || s.client == nil || s.client.capabilities == nil {
		return ports.CapabilityStatus{Capability: capability, Support: ports.CapabilityUnknown}
	}
	r := s.client.capabilities
	r.mu.RLock()
	defer r.mu.RUnlock()
	if status, ok := r.status[capability]; ok {
		return status
	}
	return ports.CapabilityStatus{Capability: capability, Support: ports.CapabilityUnknown}
}

// All returns known statuses in stable capability-name order.
func (s *Capabilities) All() []ports.CapabilityStatus {
	if s == nil || s.client == nil || s.client.capabilities == nil {
		return nil
	}
	r := s.client.capabilities
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ports.CapabilityStatus, 0, len(r.status))
	for _, status := range r.status {
		result = append(result, status)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Capability < result[j].Capability })
	return result
}

func (c *Client) requireCapability(capability ports.Capability) error {
	if c == nil || c.capabilities == nil {
		return nil
	}
	status := c.Capabilities.Support(capability)
	if status.Support != ports.CapabilityUnsupported {
		return nil
	}
	profile := c.Capabilities.Profile()
	return &ports.UnsupportedCapabilityError{Capability: capability, Track: profile.Track, Build: profile.Build}
}

func (c *Client) observeCapability(capability ports.Capability) {
	if c == nil || c.capabilities == nil {
		return
	}
	r := c.capabilities
	r.mu.Lock()
	defer r.mu.Unlock()
	configured, exists := r.status[capability]
	if exists && configured.Source == "configured profile" && configured.Support != ports.CapabilityUnknown {
		return
	}
	r.status[capability] = ports.CapabilityStatus{Capability: capability, Support: ports.CapabilitySupported, Source: "successful API response"}
}

func (c *Client) recordVersion(version *ports.APIVersion) {
	if c == nil || c.capabilities == nil || version == nil {
		return
	}
	r := c.capabilities
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.profile.Build == "" {
		r.profile.Build = version.Build
	}
	if r.profile.Release == "" {
		r.profile.Release = version.Release
	}
}

func cloneCapabilityFeatures(source map[ports.Capability]ports.CapabilitySupport) map[ports.Capability]ports.CapabilitySupport {
	if source == nil {
		return nil
	}
	result := make(map[ports.Capability]ports.CapabilitySupport, len(source))
	for capability, support := range source {
		result[capability] = support
	}
	return result
}

var _ error = (*ports.UnsupportedCapabilityError)(nil)
