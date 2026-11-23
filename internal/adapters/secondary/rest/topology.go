package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

type Topology service

func (s *Topology) List(ctx context.Context, snapshotID string) ([]ports.TopologyLink, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	base, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.TopologyLink]{Keys: []string{"links", "items", "results", "data"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, base+"/topology", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Topology.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// Deprecated: Forward deprecated the snapshot-scoped link override routes in 26.10 for removal in 26.11; use SnapshotOverrides to read one snapshot's overrides (NetworkOverrides reads the staged ones for the next snapshot).
func (s *Topology) Overrides(ctx context.Context, snapshotID string) (*ports.TopologyOverrides, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	base, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, base+"/topology/overrides", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Topology.Overrides")
	out := new(ports.TopologyOverrides)
	response, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, response, err
	}
	if out.Absent == nil {
		out.Absent = []ports.TopologyOverrideLink{}
	}
	if out.Present == nil {
		out.Present = []ports.TopologyOverrideLink{}
	}
	return out, response, err
}

// Deprecated: Forward deprecated the snapshot-scoped link override routes in 26.10 for removal in 26.11; use EditNetworkOverrides or SetNetworkOverrides, which stage overrides for the next snapshot and leave existing snapshots alone until BackdateLinkOverrides (this route wrote to the snapshot and invalidated it).
func (s *Topology) EditOverrides(ctx context.Context, snapshotID string, input ports.TopologyOverridesEdit) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *Topology) BackdateLinkOverrides(ctx context.Context, networkID, snapshotID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *Topology) NetworkOverrides(ctx context.Context, networkID string) (*ports.TopologyOverrides, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/link-overrides", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Topology.NetworkOverrides")
	out := new(ports.TopologyOverrides)
	response, err := s.client.doRequired(req, out)
	if err != nil {
		// Not an empty set: SetNetworkOverrides replaces everything staged, so a caller that ignored this error and wrote back the
		// "empty" read would wipe the overrides.
		return nil, response, err
	}
	if out.Absent == nil {
		out.Absent = []ports.TopologyOverrideLink{}
	}
	if out.Present == nil {
		out.Present = []ports.TopologyOverrideLink{}
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
func (s *Topology) StagedOverrideChanges(ctx context.Context, networkID string) (*ports.TopologyOverridesEdit, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/link-overrides?view=staged", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Topology.StagedOverrideChanges")
	out := new(ports.TopologyOverridesEdit)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// SetNetworkOverrides REPLACES all of the network's staged link overrides with
// overrides. Existing snapshots are untouched; the next snapshot uses them, and
// BackdateLinkOverrides applies them to an existing one. A nil list is sent as
// [] (Forward rejects a missing one), and a link in both lists is refused by
// Forward. PUT /api/networks/{id}/link-overrides
// (TopologyController.putLinkOverrides; EDIT_TOPOLOGY_LINKS).
func (s *Topology) SetNetworkOverrides(ctx context.Context, networkID string, overrides ports.TopologyOverrides) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	if overrides.Absent == nil {
		overrides.Absent = []ports.TopologyOverrideLink{}
	}
	if overrides.Present == nil {
		overrides.Present = []ports.TopologyOverrideLink{}
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
func (s *Topology) EditNetworkOverrides(ctx context.Context, networkID string, edit ports.TopologyOverridesEdit) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *Topology) SnapshotOverrides(ctx context.Context, networkID, snapshotID string) (*ports.TopologyOverrides, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.TopologyOverrides)
	response, err := s.client.doRequired(req, out)
	if err != nil {
		// Not an empty set: SetNetworkOverrides replaces everything staged, so a caller that ignored this error and wrote back the
		// "empty" read would wipe the overrides.
		return nil, response, err
	}
	if out.Absent == nil {
		out.Absent = []ports.TopologyOverrideLink{}
	}
	if out.Present == nil {
		out.Present = []ports.TopologyOverrideLink{}
	}
	return out, response, err
}
