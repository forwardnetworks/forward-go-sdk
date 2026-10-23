package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type TopologyService service

type TopologyLink struct {
	SourcePort string `json:"sourcePort"`
	TargetPort string `json:"targetPort"`
}
type TopologyOverrideLink struct {
	Port1 string `json:"port1"`
	Port2 string `json:"port2"`
}
type TopologyOverrides struct {
	Absent  []TopologyOverrideLink `json:"absent"`
	Present []TopologyOverrideLink `json:"present"`
}
type TopologyOverridesEdit struct {
	AbsentAdditions  []TopologyOverrideLink `json:"absentAdditions,omitempty"`
	AbsentRemovals   []TopologyOverrideLink `json:"absentRemovals,omitempty"`
	PresentAdditions []TopologyOverrideLink `json:"presentAdditions,omitempty"`
	PresentRemovals  []TopologyOverrideLink `json:"presentRemovals,omitempty"`
}

func (s *TopologyService) List(ctx context.Context, snapshotID string) ([]TopologyLink, *Response, error) {
	base, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[TopologyLink]{Keys: []string{"links", "items", "results", "data"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, base+"/topology", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Topology.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// Deprecated: Forward deprecated the snapshot-scoped link override routes in 26.10 for removal in 26.11; use SnapshotOverrides to read one snapshot's overrides (NetworkOverrides reads the staged ones for the next snapshot).
func (s *TopologyService) Overrides(ctx context.Context, snapshotID string) (*TopologyOverrides, *Response, error) {
	base, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, base+"/topology/overrides", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Topology.Overrides")
	out := new(TopologyOverrides)
	response, err := s.client.doRequired(req, out)
	if out.Absent == nil {
		out.Absent = []TopologyOverrideLink{}
	}
	if out.Present == nil {
		out.Present = []TopologyOverrideLink{}
	}
	return out, response, err
}

// Deprecated: Forward deprecated the snapshot-scoped link override routes in 26.10 for removal in 26.11; use EditNetworkOverrides or SetNetworkOverrides, which stage overrides for the next snapshot and leave existing snapshots alone until BackdateLinkOverrides (this route wrote to the snapshot and invalidated it).
func (s *TopologyService) EditOverrides(ctx context.Context, snapshotID string, input TopologyOverridesEdit) (*Response, error) {
	base, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, base+"/topology/overrides", input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Topology.EditOverrides")
	return s.client.Do(req, nil)
}
func topologySnapshotPath(snapshotID string) (string, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return "", errors.New("forward: snapshot ID is required")
	}
	return "/api/snapshots/" + url.PathEscape(snapshotID), nil
}

// BackdateLinkOverrides applies the network's STAGED link overrides from
// snapshotID onward and INVALIDATES every snapshot from that one on, so they
// reprocess and their answers are unavailable until they finish; the network
// has no staged overrides afterwards. POST
// /api/networks/{id}/link-overrides?action=backdate&snapshotId=
// (LinkOverridesService.backdateStagedLinkOverridesTo; on primary 15398425a69
// and stable 67e89c87124). Needs EDIT_TOPOLOGY_LINKS and INVALIDATE_SNAPSHOTS.
// Forward offers no route that lists the snapshots this would invalidate: it
// invalidates every non-draft snapshot of the network created at or after
// snapshotID's creation time, with no end.
func (s *TopologyService) BackdateLinkOverrides(ctx context.Context, networkID, snapshotID string) (*Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	return backdate(ctx, s.client, "/api/networks/"+url.PathEscape(networkID)+"/link-overrides", "action", snapshotID, "Topology.BackdateLinkOverrides")
}

// NetworkOverrides returns the network's STAGED link overrides: the ones the
// network's next snapshot will use. Existing snapshots are not affected by
// changes to them until BackdateLinkOverrides. GET
// /api/networks/{id}/link-overrides (TopologyController.getLinkOverrides;
// VIEW_NETWORK_AND_SNAPSHOTS). Forward 26.8's network-level replacement for the
// snapshot-scoped Overrides, which is deprecated for removal.
func (s *TopologyService) NetworkOverrides(ctx context.Context, networkID string) (*TopologyOverrides, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/link-overrides", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Topology.NetworkOverrides")
	out := new(TopologyOverrides)
	response, err := s.client.doRequired(req, out)
	if out.Absent == nil {
		out.Absent = []TopologyOverrideLink{}
	}
	if out.Present == nil {
		out.Present = []TopologyOverrideLink{}
	}
	return out, response, err
}

// StagedOverrideChanges returns the link-override changes staged since the
// network's latest processed snapshot, as the edit that turns that snapshot's
// overrides into the staged ones. It is the whole change only for a backdate to
// that latest snapshot: backdating to an older one replaces its overrides with the
// full staged set, which can change more than this edit shows. GET
// /api/networks/{id}/link-overrides?view=staged
// (TopologyController.getRecentLinkOverrideChanges; LinkOverridesService
// .getStagedLinkOverridesEdit). A link that moved between present and absent
// appears only as an addition.
func (s *TopologyService) StagedOverrideChanges(ctx context.Context, networkID string) (*TopologyOverridesEdit, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/link-overrides?view=staged", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Topology.StagedOverrideChanges")
	out := new(TopologyOverridesEdit)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// SetNetworkOverrides REPLACES all of the network's staged link overrides with
// overrides. Existing snapshots are untouched; the next snapshot uses them, and
// BackdateLinkOverrides applies them to an existing one. A nil list is sent as
// [] (Forward rejects a missing one), and a link in both lists is refused by
// Forward. PUT /api/networks/{id}/link-overrides
// (TopologyController.putLinkOverrides; EDIT_TOPOLOGY_LINKS).
func (s *TopologyService) SetNetworkOverrides(ctx context.Context, networkID string, overrides TopologyOverrides) (*Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	if overrides.Absent == nil {
		overrides.Absent = []TopologyOverrideLink{}
	}
	if overrides.Present == nil {
		overrides.Present = []TopologyOverrideLink{}
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, "/api/networks/"+url.PathEscape(networkID)+"/link-overrides", overrides)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Topology.SetNetworkOverrides")
	return s.client.Do(req, nil)
}

// EditNetworkOverrides applies an incremental edit to the network's staged link
// overrides (same body as the snapshot-scoped EditOverrides). Existing snapshots
// are untouched; see BackdateLinkOverrides. An addition to one list supersedes
// the link's override in the other. Forward refuses (400) an edit in which
// presentAdditions overlaps presentRemovals or absentAdditions, or absentAdditions
// overlaps absentRemovals; other overlaps are accepted. Both this and
// SetNetworkOverrides are checked against the latest processed snapshot's ports
// and answer 400 "Illegal ports in link overrides" for an unknown one.
// PATCH /api/networks/{id}/link-overrides (TopologyController.patchLinkOverrides;
// EDIT_TOPOLOGY_LINKS).
func (s *TopologyService) EditNetworkOverrides(ctx context.Context, networkID string, edit TopologyOverridesEdit) (*Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/networks/"+url.PathEscape(networkID)+"/link-overrides", edit)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Topology.EditNetworkOverrides")
	return s.client.Do(req, nil)
}

// SnapshotOverrides returns the link overrides in effect for one snapshot, through the network-level route that
// replaces the deprecated snapshot-scoped Overrides. GET /api/networks/{id}/link-overrides?view=snapshot&snapshotId=
// (TopologyController.getLinkOverridesAtSnapshot; VIEW_NETWORK_AND_SNAPSHOTS; Forward's 26.8 migration guide maps
// the old GET to it).
func (s *TopologyService) SnapshotOverrides(ctx context.Context, networkID, snapshotID string) (*TopologyOverrides, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	q := url.Values{"view": []string{"snapshot"}, "snapshotId": []string{snapshotID}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/link-overrides?"+q.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Topology.SnapshotOverrides")
	out := new(TopologyOverrides)
	response, err := s.client.doRequired(req, out)
	if out.Absent == nil {
		out.Absent = []TopologyOverrideLink{}
	}
	if out.Present == nil {
		out.Present = []TopologyOverrideLink{}
	}
	return out, response, err
}
