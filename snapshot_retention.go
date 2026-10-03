package forward

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// SnapshotRetentionGranularity is how many of a time period's snapshots
// Forward keeps when it thins a network's snapshots: ALL keeps every one,
// ONE_PER_DAY (TWO_DAYS, WEEK, TWO_WEEKS, MONTH, QUARTER) keeps one per
// interval, and NONE deletes the lot. They run from fine to coarse, and a
// period may not be finer than the one before it.
type SnapshotRetentionGranularity string

const (
	SnapshotRetainAll        SnapshotRetentionGranularity = "ALL"
	SnapshotRetainPerDay     SnapshotRetentionGranularity = "ONE_PER_DAY"
	SnapshotRetainPerTwoDays SnapshotRetentionGranularity = "ONE_PER_TWO_DAYS"
	SnapshotRetainPerWeek    SnapshotRetentionGranularity = "ONE_PER_WEEK"
	SnapshotRetainPerTwoWeek SnapshotRetentionGranularity = "ONE_PER_TWO_WEEKS"
	SnapshotRetainPerMonth   SnapshotRetentionGranularity = "ONE_PER_MONTH"
	SnapshotRetainPerQuarter SnapshotRetentionGranularity = "ONE_PER_QUARTER"
	SnapshotRetainNone       SnapshotRetentionGranularity = "NONE"
)

func (g SnapshotRetentionGranularity) valid() bool {
	switch g {
	case SnapshotRetainAll, SnapshotRetainPerDay, SnapshotRetainPerTwoDays, SnapshotRetainPerWeek,
		SnapshotRetainPerTwoWeek, SnapshotRetainPerMonth, SnapshotRetainPerQuarter, SnapshotRetainNone:
		return true
	}
	return false
}

// SnapshotRetentionPolicy is a network's snapshot thinning policy
// (SnapshotRetentionPolicy). Each period applies to snapshots of that age:
// LastWeek is 0-7 days, LastMonth 7-30, LastQuarter 30-90, LastYear 90-365 and
// Older over a year. With Enabled false nothing is ever thinned.
//
// Forward's rules, which it enforces with a 400: LastWeek must be ALL; each
// period may not be finer than the one before it; LastMonth may be ALL,
// ONE_PER_DAY, ONE_PER_TWO_DAYS or ONE_PER_WEEK; LastQuarter ALL, ONE_PER_DAY,
// ONE_PER_WEEK, ONE_PER_TWO_WEEKS or NONE; LastYear ONE_PER_DAY, ONE_PER_WEEK,
// ONE_PER_MONTH or NONE; Older those plus ONE_PER_QUARTER.
//
// The on-prem default, which applies until one is saved: enabled, ALL for a
// week, ONE_PER_DAY for a month, ONE_PER_WEEK for a quarter, ONE_PER_MONTH for
// a year, ONE_PER_QUARTER after. SaaS has one fixed policy (ONE_PER_WEEK from a
// quarter on) that cannot be changed.
type SnapshotRetentionPolicy struct {
	Enabled     bool                         `json:"enabled"`
	LastWeek    SnapshotRetentionGranularity `json:"lastWeek"`
	LastMonth   SnapshotRetentionGranularity `json:"lastMonth"`
	LastQuarter SnapshotRetentionGranularity `json:"lastQuarter"`
	LastYear    SnapshotRetentionGranularity `json:"lastYear"`
	Older       SnapshotRetentionGranularity `json:"older"`
}

// GetSnapshotRetentionPolicy returns the network's snapshot thinning policy:
// the saved one, or the deployment's default. GET
// /api/networks/{networkId}/snapshotRetentionPolicy
// (SnapshotRetentionController; VIEW_COLLECTION_SETTINGS). Preview: not in the
// published spec.
func (s *NetworksService) GetSnapshotRetentionPolicy(ctx context.Context, networkID string) (*SnapshotRetentionPolicy, *Response, error) {
	path, err := s.retentionPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.GetSnapshotRetentionPolicy")
	out := new(SnapshotRetentionPolicy)
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
func (s *NetworksService) SetSnapshotRetentionPolicy(ctx context.Context, networkID string, policy SnapshotRetentionPolicy) (*Response, error) {
	for name, g := range map[string]SnapshotRetentionGranularity{
		"lastWeek": policy.LastWeek, "lastMonth": policy.LastMonth, "lastQuarter": policy.LastQuarter,
		"lastYear": policy.LastYear, "older": policy.Older,
	} {
		if !g.valid() {
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
	if isStatus(err, http.StatusNotFound) {
		return resp, fmt.Errorf("forward: saving a snapshot retention policy failed with 404: the network does not exist, or this is a SaaS deployment, where the policy is fixed: %w", err)
	}
	return resp, err
}

// SnapshotRetentionPreview is what the next thinning pass would delete.
// Snapshots are listed as Forward's snapshot metadata (id, createdAt, note,
// and so on); Count equals len(Snapshots).
type SnapshotRetentionPreview struct {
	Count     int        `json:"count"`
	Snapshots []Snapshot `json:"deletedSnapshots"`
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
func (s *NetworksService) PreviewSnapshotRetention(ctx context.Context, networkID string) (*SnapshotRetentionPreview, *Response, error) {
	path, err := s.retentionPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/trigger?"+url.Values{"dryRun": []string{"true"}}.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.PreviewSnapshotRetention")
	out := new(SnapshotRetentionPreview)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

func (s *NetworksService) retentionPath(networkID string) (string, error) {
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	return path + "/snapshotRetentionPolicy", nil
}
