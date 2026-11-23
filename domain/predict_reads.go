package domain

import (
	"encoding/json"
	"errors"
	"strings"
)

// PredictProtocol names a protocol for AllowedCommands (Forward's ProtocolConfigType). Predict MODELS only BGP, OSPF, IS_IS, OMP and CONFIG
// (static routes, interface shutdown), each only when its organization property is on (PREDICT_MODEL_BGP, _OSPF, _ISIS, _OMP, _CONFIG;
// ProtocolConfigUtils). EIGRP, RIP and STP are named by Forward's type but Predict never models them: commands for them change the
// predicted config text and nothing else. With PREDICT_MODEL_CONFIG off the config text still changes but the RIB and forwarding state
// do not.
//
// PREDICT_MODEL_ISIS and PREDICT_MODEL_OMP default to OFF. Their data is parsed into each snapshot when it is processed, so after turning
// one on, a snapshot taken before needs reprocessing (unverified) before Predict can model that protocol. IS-IS comes from the Cisco-like
// vendor family; OMP is Cisco IOS-XE SD-WAN only (not Viptela vEdge), and needs a system IP and a site id on the device. Redistribution
// between protocols (including into and out of OMP and IS-IS) also needs PREDICT_MODEL_CROSS_PROTOCOL, which defaults on. Trace OMP and
// IS-IS routes with RoutingService.RPPrefixSearch and friends; Forward has no OMP or IS-IS node-read route.
type PredictProtocol string

const (
	PredictProtocolBGP    PredictProtocol = "BGP"
	PredictProtocolOSPF   PredictProtocol = "OSPF"
	PredictProtocolISIS   PredictProtocol = "IS_IS"
	PredictProtocolEIGRP  PredictProtocol = "EIGRP"
	PredictProtocolRIP    PredictProtocol = "RIP"
	PredictProtocolConfig PredictProtocol = "CONFIG"
	PredictProtocolOMP    PredictProtocol = "OMP"
	PredictProtocolSTP    PredictProtocol = "STP"
)

// predictCommandOSes are the vendor OSes Predict models CLI commands for (Forward's ConfigGenVendorOs).
var predictCommandOSes = []string{
	"cisco_ios", "cisco_ios_xe", "cisco_nxos", "cisco_ios_xr", "cisco_asa", "arista_eos", "juniper_junos", "juniper_srx",
}

// PredictCommandOSes returns the vendor OS names Predict models CLI commands for (Forward's ConfigGenVendorOs, named as PredictOSes
// names them). A change set for any other CLI OS fails when it runs ("Device ... OS ... not supported"), so check before staging.
// PAN-OS is not here: its changes are security rules (AddSecurityRule, EditSecurityRule and friends) or a whole config upload.
func PredictCommandOSes() []string { return append([]string(nil), predictCommandOSes...) }

// SupportsPredictCommands reports whether Predict models CLI commands for the vendor OS name (case-insensitive).
func SupportsPredictCommands(vendorOS string) bool {
	vendorOS = strings.ToLower(strings.TrimSpace(vendorOS))
	for _, name := range predictCommandOSes {
		if name == vendorOS {
			return true
		}
	}
	return false
}

// FirewallObjectKind names one of the firewall object collections of a change set's baseline snapshot.
type FirewallObjectKind string

const (
	FirewallAddressObjects     FirewallObjectKind = "address-objects"
	FirewallAddressGroups      FirewallObjectKind = "address-groups"
	FirewallApplicationObjects FirewallObjectKind = "application-objects"
	FirewallApplicationGroups  FirewallObjectKind = "application-groups"
	FirewallURLObjects         FirewallObjectKind = "url-objects"
	FirewallServiceObjects     FirewallObjectKind = "service-objects"
	FirewallServiceGroups      FirewallObjectKind = "service-groups"
	FirewallUserObjects        FirewallObjectKind = "user-objects"
	FirewallZones              FirewallObjectKind = "zones"
)

// BGPAdvertisementDiff is one staged BGP change set against the device's base snapshot (BgpAdvertisementDiffInfo). Change is INJECTED
// (new key), MODIFIED (same key, different attributes), DUPLICATED (already there, identical), WITHDRAWN (a received advertisement
// removed) or ABSENT (a withdrawal of something the base snapshot does not have). The advertisements are raw JSON.
type BGPAdvertisementDiff struct {
	Change        string          `json:"change"`
	Original      json.RawMessage `json:"original,omitempty"`
	UpdateAdded   json.RawMessage `json:"updateAdded,omitempty"`
	UpdateRemoved json.RawMessage `json:"updateRemoved,omitempty"`
}

// ErrNoAffectedDevices is returned by AffectedDevices for a snapshot that is not a predicted one: Forward answers a payload
// {"error": "No affected devices since this is a base snapshot"} instead of a list.
var ErrNoAffectedDevices = errors.New("forward: the snapshot has no affected devices (it is not a predicted snapshot)")

// ErrAllowedCommandsUnavailable is AllowedCommands's answer when Forward fails (500) to answer for the OS. For the Cisco-like OSes and
// Arista EOS that is Forward's own bug in rendering their command trees (see AllowedCommands), not a request fault.
var ErrAllowedCommandsUnavailable = errors.New("forward: Forward failed (500) to answer allowed-commands for this OS")

// NQECheckCommit is the latest commit of a check's query.
type NQECheckCommit struct {
	ID          Identifier `json:"id,omitempty"`
	CommittedAt string     `json:"committedAt,omitempty"`
	Path        string     `json:"path,omitempty"`
	Intent      string     `json:"intent,omitempty"`
	HasParams   bool       `json:"hasParams,omitempty"`
	// Forward flattens the commit author into the commit.
	AuthorID    Identifier `json:"authorId,omitempty"`
	AuthorEmail string     `json:"authorEmail,omitempty"`
}

// NQECheckInfo is a query enabled as an NQE check (NqeCheckInfo). Forward leaves false booleans out of the body.
type NQECheckInfo struct {
	QueryID                 string           `json:"queryId"`
	PresentInHead           bool             `json:"presentInHead,omitempty"`
	Restricted              bool             `json:"restricted,omitempty"`
	Repository              string           `json:"repository,omitempty"`
	LastCommitSourceCodeSha string           `json:"lastCommitSourceCodeSha,omitempty"`
	Commits                 []NQECheckCommit `json:"commits,omitempty"`
}
