package forward

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Capability names a Forward feature whose availability or wire contract can
// differ between appserver builds.
type Capability string

const (
	CapabilityWorkspaceNetworks           Capability = "workspace-networks"
	CapabilitySnapshotSubsetExport        Capability = "snapshot-subset-export"
	CapabilitySnapshotMultipartMerge      Capability = "snapshot-multipart-merge"
	CapabilityPersistentSnapshotChecks    Capability = "persistent-snapshot-checks"
	CapabilityPredict                     Capability = "predict"
	CapabilityStructuredBGPAdvertisements Capability = "structured-bgp-advertisements"
	CapabilityCollectorProgress           Capability = "collector-progress"
)

// CapabilitySupport is deliberately tri-state. Unknown means the SDK has not
// been given a build profile and will attempt the endpoint; it never guesses
// that primary and stable expose the same contract.
type CapabilitySupport string

const (
	CapabilityUnknown     CapabilitySupport = "unknown"
	CapabilitySupported   CapabilitySupport = "supported"
	CapabilityUnsupported CapabilitySupport = "unsupported"
)

// CapabilityProfile describes one deployment track/build. Features should be
// populated from a tested compatibility matrix maintained by the consumer.
// Successful SDK calls add runtime evidence without mutating this input value.
type CapabilityProfile struct {
	Track    string
	Build    string
	Release  string
	Features map[Capability]CapabilitySupport
}

// CapabilityStatus is the current support state and how it was learned.
type CapabilityStatus struct {
	Capability Capability
	Support    CapabilitySupport
	Source     string
}

// UnsupportedCapabilityError is returned before a request when the selected
// build profile explicitly marks a feature unsupported.
type UnsupportedCapabilityError struct {
	Capability Capability
	Track      string
	Build      string
}

func (e *UnsupportedCapabilityError) Error() string {
	if e == nil {
		return "forward: unsupported capability"
	}
	detail := strings.TrimSpace(strings.Join([]string{e.Track, e.Build}, " "))
	if detail == "" {
		return fmt.Sprintf("forward: capability %q is unsupported", e.Capability)
	}
	return fmt.Sprintf("forward: capability %q is unsupported by %s", e.Capability, detail)
}

// CapabilitiesService exposes the configured profile plus runtime evidence.
type CapabilitiesService service

type capabilityRegistry struct {
	mu      sync.RWMutex
	profile CapabilityProfile
	status  map[Capability]CapabilityStatus
}

func newCapabilityRegistry(profile CapabilityProfile) *capabilityRegistry {
	profile.Features = cloneCapabilityFeatures(profile.Features)
	r := &capabilityRegistry{profile: profile, status: make(map[Capability]CapabilityStatus)}
	for capability, support := range profile.Features {
		if support == "" {
			support = CapabilityUnknown
		}
		r.status[capability] = CapabilityStatus{Capability: capability, Support: support, Source: "configured profile"}
	}
	return r
}

// Profile returns a copy of the selected deployment profile.
func (s *CapabilitiesService) Profile() CapabilityProfile {
	if s == nil || s.client == nil || s.client.capabilities == nil {
		return CapabilityProfile{}
	}
	r := s.client.capabilities
	r.mu.RLock()
	defer r.mu.RUnlock()
	profile := r.profile
	profile.Features = cloneCapabilityFeatures(profile.Features)
	return profile
}

// Support reports whether capability is supported, unsupported, or unknown.
func (s *CapabilitiesService) Support(capability Capability) CapabilityStatus {
	if s == nil || s.client == nil || s.client.capabilities == nil {
		return CapabilityStatus{Capability: capability, Support: CapabilityUnknown}
	}
	r := s.client.capabilities
	r.mu.RLock()
	defer r.mu.RUnlock()
	if status, ok := r.status[capability]; ok {
		return status
	}
	return CapabilityStatus{Capability: capability, Support: CapabilityUnknown}
}

// All returns known statuses in stable capability-name order.
func (s *CapabilitiesService) All() []CapabilityStatus {
	if s == nil || s.client == nil || s.client.capabilities == nil {
		return nil
	}
	r := s.client.capabilities
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]CapabilityStatus, 0, len(r.status))
	for _, status := range r.status {
		result = append(result, status)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Capability < result[j].Capability })
	return result
}

func (c *Client) requireCapability(capability Capability) error {
	if c == nil || c.capabilities == nil {
		return nil
	}
	status := c.Capabilities.Support(capability)
	if status.Support != CapabilityUnsupported {
		return nil
	}
	profile := c.Capabilities.Profile()
	return &UnsupportedCapabilityError{Capability: capability, Track: profile.Track, Build: profile.Build}
}

func (c *Client) observeCapability(capability Capability) {
	if c == nil || c.capabilities == nil {
		return
	}
	r := c.capabilities
	r.mu.Lock()
	defer r.mu.Unlock()
	configured, exists := r.status[capability]
	if exists && configured.Source == "configured profile" && configured.Support != CapabilityUnknown {
		return
	}
	r.status[capability] = CapabilityStatus{Capability: capability, Support: CapabilitySupported, Source: "successful API response"}
}

func (c *Client) recordVersion(version *APIVersion) {
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

func cloneCapabilityFeatures(source map[Capability]CapabilitySupport) map[Capability]CapabilitySupport {
	if source == nil {
		return nil
	}
	result := make(map[Capability]CapabilitySupport, len(source))
	for capability, support := range source {
		result[capability] = support
	}
	return result
}

var _ error = (*UnsupportedCapabilityError)(nil)
