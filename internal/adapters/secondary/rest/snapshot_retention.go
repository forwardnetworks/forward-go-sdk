package rest

import (
	"context"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
)

// GetSnapshotRetentionPolicy returns the network's snapshot thinning policy:
// the saved one, or the deployment's default. GET
// /api/networks/{networkId}/snapshotRetentionPolicy
// (SnapshotRetentionController; VIEW_COLLECTION_SETTINGS). Preview: not in the
// published spec.
func (s *Networks) GetSnapshotRetentionPolicy(ctx context.Context, networkID string) (*ports.SnapshotRetentionPolicy, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.retentionPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.GetSnapshotRetentionPolicy")
	out := new(ports.SnapshotRetentionPolicy)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// SetSnapshotRetentionPolicy replaces the network's snapshot thinning policy.
// PUT /api/networks/{networkId}/snapshotRetentionPolicy (SnapshotRetentionController;
// EDIT_COLLECTION_SETTINGS and DELETE_SNAPSHOT). Preview.
//
// It changes which snapshots the daily cleanup deletes, so a stricter policy
// is a deletion later; PreviewSnapshotRetention shows what it would take. To
// stop thinning a network, GetSnapshotRetentionPolicy, set Enabled false and
// save it back: that keeps the other fields valid, which Forward requires even
// for a disabled policy.
//
// Only ON-PREM deployments accept this. On SaaS the policy is fixed and Forward
// answers 404 ("policy modification not allowed on cloud"), which is also what
// an unknown network gets; the error says so and still satisfies
// IsStatus(err, 404). Thinning is also switched off for a whole organization by
// the SNAPSHOT_AUTO_CLEANUP org property (see PreviewSnapshotRetention).
func (s *Networks) SetSnapshotRetentionPolicy(ctx context.Context, networkID string, policy ports.SnapshotRetentionPolicy) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	for name, g := range map[string]ports.SnapshotRetentionGranularity{
		"lastWeek": policy.LastWeek, "lastMonth": policy.LastMonth, "lastQuarter": policy.LastQuarter,
		"lastYear": policy.LastYear, "older": policy.Older,
	} {
		if !g.Valid() {
			return nil, fmt.Errorf("forward: snapshot retention %s has invalid granularity %q", name, g)
		}
	}
	path, err := s.retentionPath(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path, policy)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Networks.SetSnapshotRetentionPolicy")
	resp, err := s.client.Do(req, nil)
	if ports.IsStatus(err, http.StatusNotFound) {
		return resp, fmt.Errorf("forward: saving a snapshot retention policy failed with 404: the network does not exist, or this is a SaaS deployment, where the policy is fixed: %w", err)
	}
	return resp, err
}

// PreviewSnapshotRetention lists the snapshots the network's retention policy
// would delete if the cleanup ran now, and deletes NOTHING: the request always
// carries dryRun=true. POST /api/networks/{networkId}/snapshotRetentionPolicy/trigger
// ?dryRun=true (SnapshotRetentionController; DELETE_SNAPSHOT, which even the
// preview needs). The same route without dryRun deletes synchronously, so the
// SDK does not expose that.
//
// Forward always keeps, whatever the policy: the 10 newest processed
// snapshots of the network (not forks, drafts or restored ones), every
// favorited snapshot, forks and predictions with their ancestors and bases,
// and restored snapshots newer than the RESTORED_SNAPSHOT_RETENTION_IN_DAYS
// org property. The preview applies all of those.
//
// What it does not check: the SNAPSHOT_AUTO_CLEANUP org property (default
// true; org admins cannot change it) that switches the daily 05:00 UTC
// cleanup off for every network in the org. With it false the real job
// deletes nothing although this preview may list snapshots; read it with
// Properties before warning about deletions. A disabled policy
// (Enabled false) previews as empty. Preview.
func (s *Networks) PreviewSnapshotRetention(ctx context.Context, networkID string) (*ports.SnapshotRetentionPreview, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.retentionPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/trigger?"+url.Values{"dryRun": []string{"true"}}.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.PreviewSnapshotRetention")
	out := new(ports.SnapshotRetentionPreview)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

func (s *Networks) retentionPath(networkID string) (string, error) {
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	return path + "/snapshotRetentionPolicy", nil
}
