package usecases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"maps"
	"net/http"
	"strings"
)

// BGPAdvertisementReadiness checks, before a Predict run that stages or withdraws external BGP advertisements on a device, the three
// things that otherwise make Predict model nothing without an error: the organization properties PREDICT_MODELING, PREDICT_MODEL_BGP and
// PREDICT_MODEL_EXT_ADV, the device's collection setup (collectBgpAdvertisements on, a table type that includes ADJ_RIB_IN so received
// routes exist to withdraw), and the network's requirement groups. It only reads: Properties.Current, ClassicDevices.List and
// BGPCollectionRequirements. A read that fails with a permission or not-served error becomes an Unknown entry and a note, not an error,
// so a caller without organization-configuration access still gets the rest; Ready is false while anything is Unknown. For several
// devices use BGPAdvertisementReadinessFor, which makes those reads once. Preview: every shape is from Forward's source.
func (s *Predict) BGPAdvertisementReadiness(ctx context.Context, networkID, deviceName string) (*domain.BGPReadiness, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	reports, resp, err := s.BGPAdvertisementReadinessFor(ctx, networkID, []string{deviceName})
	if err != nil {
		return nil, resp, err
	}
	return reports[0], resp, nil
}

// BGPAdvertisementReadinessFor is BGPAdvertisementReadiness for several devices of one network, in the order given. The three reads it
// is built on (the organization's properties, the network's collection sources and its requirement groups) are the same for every
// device, so they are made once however many devices are asked about: three requests, not three per device.
func (s *Predict) BGPAdvertisementReadinessFor(ctx context.Context, networkID string, deviceNames []string) ([]*domain.BGPReadiness, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	if len(deviceNames) == 0 {
		return nil, nil, errors.New("forward: at least one device name is required")
	}
	names := make([]string, len(deviceNames))
	for i, name := range deviceNames {
		names[i] = strings.TrimSpace(name)
		if _, _, err := domain.RequireDeviceRef(networkID, names[i]); err != nil {
			return nil, nil, err
		}
	}
	networkID, err := domain.ResolveNetworkID(networkID, s.session.Network())
	if err != nil {
		return nil, nil, err
	}
	var last *ports.Response
	// shared holds what every device's report shares: what could not be read (Unknown, Notes) and the org-wide findings.
	shared := &domain.BGPReadiness{UnknownReasons: map[string]string{}}
	refused := func(err error, unknown, what string) (hard error) {
		if ports.IsErrorKind(err, domain.ErrorKindPermissionDenied) || ports.IsErrorKind(err, domain.ErrorKindEndpointNotServed) || ports.IsStatus(err, http.StatusForbidden) {
			shared.Unknown = append(shared.Unknown, unknown)
			shared.UnknownReasons[unknown] = fmt.Sprintf("%s could not be read: %v", what, err)
			shared.Notes = append(shared.Notes, shared.UnknownReasons[unknown])
			return nil
		}
		return err
	}

	props, resp, err := s.properties.Current(ctx, domain.PropertyFilterOff)
	last = resp
	if err != nil {
		if hard := refused(err, "organization-properties", "organization properties"); hard != nil {
			return nil, resp, hard
		}
	} else {
		shared.PredictModeling = propertyBool(props.Lookup(domain.PropertyPredictModeling))
		shared.PredictModelExtAdv = propertyBool(props.Lookup(domain.PropertyPredictModelExtAdv))
		shared.PredictModelBGP = propertyBool(props.Lookup(domain.PropertyPredictModelBGP))
		var missing []string
		for _, p := range []struct {
			name  domain.OrgProperty
			value *bool
		}{{domain.PropertyPredictModeling, shared.PredictModeling}, {domain.PropertyPredictModelExtAdv, shared.PredictModelExtAdv}, {domain.PropertyPredictModelBGP, shared.PredictModelBGP}} {
			if p.value == nil {
				missing = append(missing, string(p.name))
			}
		}
		if len(missing) != 0 {
			shared.Unknown = append(shared.Unknown, missing...)
			for _, name := range missing {
				shared.UnknownReasons[name] = "not returned by the organization properties API (default true in Forward's source)"
			}
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

	devices, resp, err := s.classicDevices.List(ctx, networkID)
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

	reports := make([]*domain.BGPReadiness, len(names))
	for i, name := range names {
		report := &domain.BGPReadiness{
			Device:             name,
			PredictModeling:    shared.PredictModeling,
			PredictModelExtAdv: shared.PredictModelExtAdv,
			PredictModelBGP:    shared.PredictModelBGP,
			Problems:           append([]string(nil), shared.Problems...),
			Unknown:            append([]string(nil), shared.Unknown...),
			UnknownReasons:     maps.Clone(shared.UnknownReasons),
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
				key := "collection-source:" + name
				report.Unknown = append(report.Unknown, key)
				report.UnknownReasons[key] = "the network has no classic collection source named " + name + " (a controller-managed device is configured elsewhere, and this read cannot check it)"
				report.Notes = append(report.Notes, report.UnknownReasons[key])
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

func propertyBool(raw json.RawMessage) *bool {
	var b bool
	if len(raw) == 0 || json.Unmarshal(raw, &b) != nil {
		return nil
	}
	return &b
}
