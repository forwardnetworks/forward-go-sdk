package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// ControllerManagedSetupsService manages SD-WAN controller-managed setups:
// the record that tells Forward a controller (vManage, vBond, vSmart) owns a
// set of devices, rather than each device being collected classically.
//
// Preview: these routes are not in the published OpenAPI set.
type ControllerManagedSetupsService service

// ControllerManagedSetup is a setup as Forward returns it.
type ControllerManagedSetup struct {
	Name        string             `json:"name"`
	Controllers []ControllerDevice `json:"controllers"`
}

// ControllerDevice is one controller within a setup.
//
// BGPAdvertisementCollectionOptions and SNMPCollectionConfig carry the same
// settings the classic batch writes as collectBgpAdvertisements/bgpTableType/
// bgpPeerType and enableSnmpCollection. Forward unwraps the meta record into
// the controller entry, which is why they sit at this level rather than in a
// nested object of their own.
type ControllerDevice struct {
	Name                              string                `json:"name"`
	Type                              string                `json:"type,omitempty"`
	Host                              string                `json:"host,omitempty"`
	CLICredentialID                   string                `json:"cliCredentialId,omitempty"`
	SNMPCredentialID                  string                `json:"snmpCredentialId,omitempty"`
	JumpServerID                      string                `json:"jumpServerId,omitempty"`
	BGPAdvertisementCollectionOptions *BGPCollectionOptions `json:"bgpAdvertisementCollectionOptions,omitempty"`
	SNMPCollectionConfig              *SNMPCollectionConfig `json:"snmpCollectionConfig,omitempty"`
}

type BGPCollectionOptions struct {
	CollectBGPAdvertisements bool   `json:"collectBgpAdvertisements"`
	TableType                string `json:"tableType,omitempty"`
	PeerType                 string `json:"peerType,omitempty"`
}

type SNMPCollectionConfig struct {
	EnableSNMPCollection bool `json:"enableSnmpCollection"`
}

// NewControllerManagedSetup creates a setup.
//
// The name is LOWERCASED by Forward on the way in
// (NewControllerManagedSetup lowercases it in the constructor), so a caller
// that compares a returned name against the one it sent must fold case or it
// will see a difference that is not there.
type NewControllerManagedSetup struct {
	Name        string             `json:"name"`
	Controllers []ControllerDevice `json:"controllers"`
}

func (s *ControllerManagedSetupsService) base(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/controller-managed-setups", nil
}

// List returns the network's controller-managed setups.
//
// Forward wraps them as {"setups": [...]}. A bare array and the other list
// envelopes are accepted too, because a reader that knows one shape does not
// fail on another -- it returns EMPTY, and an empty list here means "no setup
// exists", which sends the caller straight into a create that then collides
// with the setup that was sitting there ("Controller-managed setup named
// 'sdwan' already exists in network", measured on cs-lab network 3150).
//
// A body that is NEITHER an array nor an object with a known key is an
// ERROR, not an empty list -- an HTML login page or {"error":...} must not
// read as "this network has no setups".
func (s *ControllerManagedSetupsService) List(ctx context.Context, networkID string) ([]ControllerManagedSetup, *Response, error) {
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	// AllowSingle is deliberately OFF. This list drives a delete-and-recreate
	// reconciliation, so a misread is not a cosmetic problem: with a single
	// object accepted, an ERROR body like {"error":"forbidden"} would decode
	// into one setup with an empty name, match nothing, and let the drifted
	// setup survive exactly as if the list had been empty. An object carrying
	// none of the known keys must therefore be an ERROR.
	result := listResponse[ControllerManagedSetup]{
		Keys: []string{"setups", "data", "items", "results"},
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ControllerManagedSetups.List")
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Create adds a controller-managed setup.
func (s *ControllerManagedSetupsService) Create(ctx context.Context, networkID string, input NewControllerManagedSetup) (*ControllerManagedSetup, *Response, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, nil, errors.New("forward: controller-managed setup name is required")
	}
	if len(input.Controllers) == 0 {
		return nil, nil, errors.New("forward: a controller-managed setup requires at least one controller")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ControllerManagedSetups.Create")
	out := new(ControllerManagedSetup)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// Delete removes a setup by name. A 404 is success: the desired state, that
// the setup is absent, already holds.
func (s *ControllerManagedSetupsService) Delete(ctx context.Context, networkID, setupName string) (*Response, error) {
	setupName = strings.TrimSpace(setupName)
	if setupName == "" {
		return nil, errors.New("forward: controller-managed setup name is required")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path+"/"+url.PathEscape(setupName), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "ControllerManagedSetups.Delete")
	resp, err := s.client.Do(req, nil)
	if isStatus(err, http.StatusNotFound) {
		return resp, nil
	}
	return resp, err
}
