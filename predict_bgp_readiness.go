package forward

import (
	"context"
	"encoding/json"
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
)

// BGPCollectionRequirement is one group of devices Forward requires BGP advertisement collection on: the network's gateways to
// adjacent networks, the internet and intranet nodes and L3 VPNs whose subnets come from BGP routes. Forward serializes only the
// device names; the table and peer type each group needs (BOTH/EBGP for adjacent-network gateways, ADJ_RIB_OUT for the others) stay
// server-side and are enforced when a device is saved.
type BGPCollectionRequirement struct {
	Devices []string `json:"devices"`
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
	// DeviceFound is false when the network has no collection source with that name (a controller-managed device is not one).
	DeviceFound              bool
	CollectBGPAdvertisements *bool
	// BGPTableType is BOTH, ADJ_RIB_IN or ADJ_RIB_OUT, with Forward's unstated default filled in as BOTH when collection is on.
	BGPTableType string
	BGPPeerType  string
	// RequiredByNetwork is true when the device is in one of the network's BGP collection requirement groups.
	RequiredByNetwork bool
	Problems          []string
	Notes             []string
}

// Ready reports that no problem was found.
func (r *BGPReadiness) Ready() bool { return r != nil && len(r.Problems) == 0 }

// BGPAdvertisementReadiness checks, before a Predict run that stages or withdraws external BGP advertisements on a device, the three
// things that otherwise make Predict model nothing without an error: the organization properties PREDICT_MODELING and
// PREDICT_MODEL_EXT_ADV, the device's collection setup (collectBgpAdvertisements on, a table type that includes ADJ_RIB_IN so received
// routes exist to withdraw), and the network's requirement groups. It only reads: Properties.Current, ClassicDevices.List and
// BGPCollectionRequirements. A read that fails with a permission or not-served error becomes a note, not an error, so a caller without
// organization-configuration access still gets the device half. Preview: every shape is from Forward's source.
func (s *PredictService) BGPAdvertisementReadiness(ctx context.Context, networkID, deviceName string) (*BGPReadiness, *Response, error) {
	deviceName = strings.TrimSpace(deviceName)
	if _, err := modeledDevicePath(networkID, deviceName); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	report := &BGPReadiness{Device: deviceName}
	var last *Response

	soft := func(err error, what string) (hard error) {
		if IsErrorKind(err, ErrorKindPermissionDenied) || IsErrorKind(err, ErrorKindEndpointNotServed) || IsStatus(err, http.StatusForbidden) {
			report.Notes = append(report.Notes, fmt.Sprintf("%s could not be read: %v", what, err))
			return nil
		}
		return err
	}

	props, resp, err := s.client.Properties.Current(ctx, PropertyFilterOff)
	last = resp
	if err != nil {
		if hard := soft(err, "organization properties"); hard != nil {
			return nil, resp, hard
		}
	} else {
		report.PredictModeling = propertyBool(props[PropertyPredictModeling])
		report.PredictModelExtAdv = propertyBool(props[PropertyPredictModelExtAdv])
	}
	if report.PredictModeling != nil && !*report.PredictModeling {
		report.Problems = append(report.Problems, "PREDICT_MODELING is off for the organization: Predict change sets are unavailable")
	}
	if report.PredictModelExtAdv != nil && !*report.PredictModelExtAdv {
		report.Problems = append(report.Problems, "PREDICT_MODEL_EXT_ADV is off: protocol modeling does not seed routes from the collected BGP received-route tables")
	}

	devices, resp, err := s.client.ClassicDevices.List(ctx, networkID)
	if resp != nil {
		last = resp
	}
	if err != nil {
		if hard := soft(err, "the network's collection sources"); hard != nil {
			return nil, last, hard
		}
	} else {
		for _, d := range devices {
			if !strings.EqualFold(d.Name, deviceName) {
				continue
			}
			report.DeviceFound = true
			report.CollectBGPAdvertisements = d.CollectBGPAdvertisements
			report.BGPTableType, report.BGPPeerType = d.BGPTableType, d.BGPPeerType
			break
		}
		switch {
		case !report.DeviceFound:
			report.Problems = append(report.Problems, "the network has no collection source named "+deviceName+" (a controller-managed device does not take this setting)")
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

	reqs, resp, err := s.BGPCollectionRequirements(ctx, networkID)
	if resp != nil {
		last = resp
	}
	if err != nil {
		if hard := soft(err, "the network's BGP collection requirements"); hard != nil {
			return nil, last, hard
		}
	} else {
		for _, group := range reqs {
			for _, name := range group.Devices {
				if strings.EqualFold(name, deviceName) {
					report.RequiredByNetwork = true
				}
			}
		}
	}
	return report, last, nil
}

func propertyBool(raw json.RawMessage) *bool {
	var b bool
	if len(raw) == 0 || json.Unmarshal(raw, &b) != nil {
		return nil
	}
	return &b
}
