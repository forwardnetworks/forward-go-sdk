package domain

// Organization properties that decide whether Predict models external BGP advertisements. PredictModeling switches Predict's protocol
// modeling on (the change-set routes need it); PredictModelExtAdv (OrgProperty.PREDICT_MODEL_EXT_ADV, default true, on-premises
// configurable) lets protocol modeling seed routes from the collected iBGP and eBGP received-route tables, so with it off a staged
// advertisement is not modeled the way a received one is.
const (
	PropertyPredictModeling    OrgProperty = "PREDICT_MODELING"
	PropertyPredictModelExtAdv OrgProperty = "PREDICT_MODEL_EXT_ADV"
	// PropertyPredictModelBGP is PREDICT_MODEL_BGP, the switch for modeling BGP at all (default true, on-premises configurable).
	PropertyPredictModelBGP OrgProperty = "PREDICT_MODEL_BGP"
)

// BGPCollectionRequirement is one group of devices Forward requires BGP advertisement collection on: the network's gateways to
// adjacent networks, the internet and intranet nodes and L3 VPNs whose subnets come from BGP routes. Devices is what Forward's
// DeviceBgpCollectionReq always serializes. The permitted table and peer types (BOTH plus the one the group requires: BOTH/EBGP for
// adjacent-network gateways, ADJ_RIB_OUT for the others) have no @JsonIgnore in Forward's source, so they are probably sent too, but
// this is not confirmed live (the lists read so far were empty); the required type itself is @JsonIgnore and never sent. Both stay
// nil when Forward sends neither.
type BGPCollectionRequirement struct {
	Devices             []string `json:"devices"`
	PermittedTableTypes []string `json:"permittedTableTypes,omitempty"`
	PermittedPeerTypes  []string `json:"permittedPeerTypes,omitempty"`
}

// BGPReadiness is BGPAdvertisementReadiness's answer. A pointer is nil when that value could not be read (the caller lacks the
// permission to read the organization's configuration, say); Notes says why. Problems lists each reason Predict would silently model
// nothing for the device; Ready is true when there is none.
type BGPReadiness struct {
	Device string
	// PredictModeling and PredictModelExtAdv are the effective values for the calling user's organization.
	PredictModeling    *bool
	PredictModelExtAdv *bool
	PredictModelBGP    *bool
	// DeviceFound is false when the network has no collection source with that name (a controller-managed device is not one).
	DeviceFound              bool
	CollectBGPAdvertisements *bool
	// BGPTableType is BOTH, ADJ_RIB_IN or ADJ_RIB_OUT, with Forward's unstated default filled in as BOTH when collection is on.
	BGPTableType string
	BGPPeerType  string
	// RequiredByNetwork is true when the device is in one of the network's BGP collection requirement groups.
	RequiredByNetwork bool
	// Problems are reasons Predict would model nothing, found by a read that succeeded. Unknown names what could NOT be checked: an
	// organization property the API did not return, or a read that was refused. Forward's source default for the three Predict
	// switches is true, but this does not assume it.
	Problems []string
	Unknown  []string
	// UnknownReasons says why each Unknown entry could not be checked, keyed by that entry's exact text.
	UnknownReasons map[string]string
	Notes          []string
}

// Ready reports that every check ran and found no problem. A value that could not be read (see Unknown) is not a pass, so on a
// deployment that hides the Predict properties from the caller Ready stays false; use Blocked for "something is definitely wrong".
func (r *BGPReadiness) Ready() bool { return r != nil && len(r.Problems) == 0 && len(r.Unknown) == 0 }

// Blocked reports that a check that ran found a problem: Predict would model nothing for the device.
func (r *BGPReadiness) Blocked() bool { return r != nil && len(r.Problems) != 0 }
