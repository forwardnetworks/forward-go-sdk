package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
)

// BGPCollectionRequirements returns the groups of devices whose collection setup must include BGP advertisements (GET
// /api/networks/{networkId}/bgp-collection-requirements, SyntheticDeviceController.getBgpCollectionRequirements,
// VIEW_NETWORK_AND_SNAPSHOTS; the body is {"requirements": [...]}). Preview: not in the published OpenAPI; source-only.
func (s *Predict) BGPCollectionRequirements(ctx context.Context, networkID string) ([]ports.BGPCollectionRequirement, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	var out struct {
		Requirements []ports.BGPCollectionRequirement `json:"requirements"`
	}
	resp, err := s.doJSON(ctx, http.MethodGet, path+"/bgp-collection-requirements", nil, &out, "Predict.BGPCollectionRequirements")
	if err != nil {
		return nil, resp, err
	}
	if out.Requirements == nil {
		out.Requirements = []ports.BGPCollectionRequirement{}
	}
	return out.Requirements, resp, nil
}
