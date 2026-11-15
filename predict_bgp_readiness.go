package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

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

// BGPCollectionRequirements returns the groups of devices whose collection setup must include BGP advertisements (GET
// /api/networks/{networkId}/bgp-collection-requirements, SyntheticDeviceController.getBgpCollectionRequirements,
// VIEW_NETWORK_AND_SNAPSHOTS; the body is {"requirements": [...]}). Preview: not in the published OpenAPI; source-only.
func (s *PredictService) BGPCollectionRequirements(ctx context.Context, networkID string) ([]BGPCollectionRequirement, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	var out struct {
		Requirements []BGPCollectionRequirement `json:"requirements"`
	}
	resp, err := s.doJSON(ctx, http.MethodGet, path+"/bgp-collection-requirements", nil, &out, "Predict.BGPCollectionRequirements")
	if err != nil {
		return nil, resp, err
	}
	if out.Requirements == nil {
		out.Requirements = []BGPCollectionRequirement{}
	}
	return out.Requirements, resp, nil
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
	Notes    []string
}

// Ready reports that every check ran and found no problem. A value that could not be read (see Unknown) is not a pass, so on a
// deployment that hides the Predict properties from the caller Ready stays false; use Blocked for "something is definitely wrong".
func (r *BGPReadiness) Ready() bool { return r != nil && len(r.Problems) == 0 && len(r.Unknown) == 0 }

// Blocked reports that a check that ran found a problem: Predict would model nothing for the device.
func (r *BGPReadiness) Blocked() bool { return r != nil && len(r.Problems) != 0 }

// BGPAdvertisementReadiness checks, before a Predict run that stages or withdraws external BGP advertisements on a device, the three
// things that otherwise make Predict model nothing without an error: the organization properties PREDICT_MODELING, PREDICT_MODEL_BGP and
// PREDICT_MODEL_EXT_ADV, the device's collection setup (collectBgpAdvertisements on, a table type that includes ADJ_RIB_IN so received
// routes exist to withdraw), and the network's requirement groups. It only reads: Properties.Current, ClassicDevices.List and
// BGPCollectionRequirements. A read that fails with a permission or not-served error becomes an Unknown entry and a note, not an error,
// so a caller without organization-configuration access still gets the rest; Ready is false while anything is Unknown. For several
// devices use BGPAdvertisementReadinessFor, which makes those reads once. Preview: every shape is from Forward's source.
func (s *PredictService) BGPAdvertisementReadiness(ctx context.Context, networkID, deviceName string) (*BGPReadiness, *Response, error) {
	reports, resp, err := s.BGPAdvertisementReadinessFor(ctx, networkID, []string{deviceName})
	if err != nil {
		return nil, resp, err
	}
	return reports[0], resp, nil
}

// BGPAdvertisementReadinessFor is BGPAdvertisementReadiness for several devices of one network, in the order given. The three reads it
// is built on (the organization's properties, the network's collection sources and its requirement groups) are the same for every
// device, so they are made once however many devices are asked about: three requests, not three per device.
func (s *PredictService) BGPAdvertisementReadinessFor(ctx context.Context, networkID string, deviceNames []string) ([]*BGPReadiness, *Response, error) {
	if len(deviceNames) == 0 {
		return nil, nil, errors.New("forward: at least one device name is required")
	}
	names := make([]string, len(deviceNames))
	for i, name := range deviceNames {
		names[i] = strings.TrimSpace(name)
		if _, err := modeledDevicePath(networkID, names[i]); err != nil {
			return nil, nil, err
		}
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	var last *Response
	// shared holds what every device's report shares: what could not be read (Unknown, Notes) and the org-wide findings.
	shared := &BGPReadiness{}
	refused := func(err error, unknown, what string) (hard error) {
		if IsErrorKind(err, ErrorKindPermissionDenied) || IsErrorKind(err, ErrorKindEndpointNotServed) || IsStatus(err, http.StatusForbidden) {
			shared.Unknown = append(shared.Unknown, unknown)
			shared.Notes = append(shared.Notes, fmt.Sprintf("%s could not be read: %v", what, err))
			return nil
		}
		return err
	}

	props, resp, err := s.client.Properties.Current(ctx, PropertyFilterOff)
	last = resp
	if err != nil {
		if hard := refused(err, "organization-properties", "organization properties"); hard != nil {
			return nil, resp, hard
		}
	} else {
		shared.PredictModeling = propertyBool(propertyRaw(props, PropertyPredictModeling))
		shared.PredictModelExtAdv = propertyBool(propertyRaw(props, PropertyPredictModelExtAdv))
		shared.PredictModelBGP = propertyBool(propertyRaw(props, PropertyPredictModelBGP))
		var missing []string
		for _, p := range []struct {
			name  OrgProperty
			value *bool
		}{{PropertyPredictModeling, shared.PredictModeling}, {PropertyPredictModelExtAdv, shared.PredictModelExtAdv}, {PropertyPredictModelBGP, shared.PredictModelBGP}} {
			if p.value == nil {
				missing = append(missing, string(p.name))
			}
		}
		if len(missing) != 0 {
			shared.Unknown = append(shared.Unknown, missing...)
			shared.Notes = append(shared.Notes, "not returned by the organization properties API (default true in Forward's source): "+strings.Join(missing, ", "))
		}
	}
	if shared.PredictModeling != nil && !*shared.PredictModeling {
		shared.Problems = append(shared.Problems, "PREDICT_MODELING is off for the organization: Predict change sets are unavailable")
	}
	if shared.PredictModelBGP != nil && !*shared.PredictModelBGP {
		shared.Problems = append(shared.Problems, "PREDICT_MODEL_BGP is off: Predict does not model BGP")
	}
	if shared.PredictModelExtAdv != nil && !*shared.PredictModelExtAdv {
		shared.Problems = append(shared.Problems, "PREDICT_MODEL_EXT_ADV is off: protocol modeling does not seed routes from the collected BGP received-route tables")
	}

	devices, resp, err := s.client.ClassicDevices.List(ctx, networkID)
	if resp != nil {
		last = resp
	}
	devicesRead := err == nil
	if err != nil {
		if hard := refused(err, "classic-devices", "the network's collection sources"); hard != nil {
			return nil, last, hard
		}
	}
	reqs, resp, err := s.BGPCollectionRequirements(ctx, networkID)
	if resp != nil {
		last = resp
	}
	reqsRead := err == nil
	if err != nil {
		if hard := refused(err, "bgp-collection-requirements", "the network's BGP collection requirements"); hard != nil {
			return nil, last, hard
		}
	}

	reports := make([]*BGPReadiness, len(names))
	for i, name := range names {
		report := &BGPReadiness{
			Device:             name,
			PredictModeling:    shared.PredictModeling,
			PredictModelExtAdv: shared.PredictModelExtAdv,
			PredictModelBGP:    shared.PredictModelBGP,
			Problems:           append([]string(nil), shared.Problems...),
			Unknown:            append([]string(nil), shared.Unknown...),
			Notes:              append([]string(nil), shared.Notes...),
		}
		if devicesRead {
			for _, d := range devices {
				if !strings.EqualFold(d.Name, name) {
					continue
				}
				report.DeviceFound = true
				report.CollectBGPAdvertisements = d.CollectBGPAdvertisements
				report.BGPTableType, report.BGPPeerType = d.BGPTableType, d.BGPPeerType
				break
			}
			switch {
			case !report.DeviceFound:
				// Not a problem found: a controller-managed device has no classic collection source and takes its BGP setting elsewhere,
				// so this cannot tell "absent" from "configured another way".
				report.Unknown = append(report.Unknown, "collection-source:"+name)
				report.Notes = append(report.Notes, "the network has no classic collection source named "+name+" (a controller-managed device is configured elsewhere, and this read cannot check it)")
			case report.CollectBGPAdvertisements == nil || !*report.CollectBGPAdvertisements:
				report.Problems = append(report.Problems, "collectBgpAdvertisements is not on for the device, so there are no received routes to override or withdraw")
			default:
				// Forward leaves bgpTableType and bgpPeerType out when they are BOTH.
				if report.BGPTableType == "" {
					report.BGPTableType = "BOTH"
				}
				if report.BGPPeerType == "" {
					report.BGPPeerType = "BOTH"
				}
				if report.BGPTableType == "ADJ_RIB_OUT" {
					report.Problems = append(report.Problems, "bgpTableType is ADJ_RIB_OUT: received routes are not collected; use BOTH or ADJ_RIB_IN")
				}
			}
		}
		if reqsRead {
			for _, group := range reqs {
				for _, groupDevice := range group.Devices {
					if strings.EqualFold(groupDevice, name) {
						report.RequiredByNetwork = true
					}
				}
			}
		}
		reports[i] = report
	}
	return reports, last, nil
}

// propertyRaw finds a property by name ignoring case: 26.11.0-03 answers /api/config with lower-case keys (predict_modeling), where the
// Java enum and Forward's documentation use upper case.
func propertyRaw(props PropertyValues, name OrgProperty) json.RawMessage {
	if raw, ok := props[name]; ok {
		return raw
	}
	for key, raw := range props {
		if strings.EqualFold(string(key), string(name)) {
			return raw
		}
	}
	return nil
}

func propertyBool(raw json.RawMessage) *bool {
	var b bool
	if len(raw) == 0 || json.Unmarshal(raw, &b) != nil {
		return nil
	}
	return &b
}
