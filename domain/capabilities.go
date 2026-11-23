package domain

import (
	"fmt"
	"strings"
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
