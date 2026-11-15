package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// BGPAdvertisements returns the BGP advertisements a device received for one prefix in one VRF, as the snapshot recorded them: the
// eBGP ones first, then the iBGP ones. GET /api/networks/{networkId}/devices/{deviceName}/bgp-advertisements?vrf=&prefix=&snapshotId=
// (DeviceController.getDeviceBgpAdvertisements, VIEW_NETWORK_AND_SNAPSHOTS; served on primary and stable). Preview: the route is not in
// the published OpenAPI; the shape is BgpAdvertisement, the same record Predict stages, so a received route and a staged one compare
// field by field. The Device field is not part of the body and stays empty.
//
// Forward requires both vrf and prefix: there is no "list everything" form, so a caller that wants a withdraw pre-check asks for the
// key's own VRF and prefix and looks for a row with the same ExternalPeer. The prefix is an IP subnet, "10.10.10.0/24"; an empty
// snapshotID lets Forward use the latest processed snapshot. A VRF or prefix with no advertisements is an empty list, not an error.
// A predicted snapshot works too, which is how the post-stage state is read.
func (s *DevicesService) BGPAdvertisements(ctx context.Context, networkID, deviceName, snapshotID, vrf, prefix string) ([]BGPAdvertisement, *Response, error) {
	path, err := modeledDevicePath(networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	vrf, prefix = strings.TrimSpace(vrf), strings.TrimSpace(prefix)
	if vrf == "" || prefix == "" {
		return nil, nil, errors.New("forward: a VRF and a prefix are required (Forward has no unfiltered form of this read)")
	}
	query := url.Values{"vrf": []string{vrf}, "prefix": []string{prefix}}
	setString(query, "snapshotId", snapshotID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/bgp-advertisements?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Devices.BGPAdvertisements")
	out := listResponse[BGPAdvertisement]{}
	resp, err := s.client.Do(req, &out)
	if err != nil {
		return nil, resp, err
	}
	if out.Items == nil {
		out.Items = []BGPAdvertisement{}
	}
	return out.Items, resp, nil
}
