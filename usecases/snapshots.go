package usecases

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
	"time"
)

// snapshotSearchLimit bounds the listing Get searches. Large enough to cover
// any recent snapshot, bounded so a network with a long history cannot make one
// lookup unbounded.
const snapshotSearchLimit = 1000

// Get returns snapshot metadata by searching the network's snapshot listing
// (archived and predicted snapshots included).
//
// Forward has no per-snapshot metadata route: GET
// /api/networks/{networkId}/snapshots/{snapshotId} is not mapped at primary
// 15398425a69 or stable 67e89c87124 (SnapshotController.java maps only
// /networks/{networkId}/snapshots and .../snapshots/latestProcessed there), and
// an appserver answers it "No endpoint GET ...". Get used to try it first and
// fall back on the 404, which cost every call -- and every poll of Operation --
// a dead request. The bare /api/snapshots/{id} route is not an alternative: it
// returns the snapshot's exported ZIP, not its metadata.
func (s *Snapshots) Get(ctx context.Context, networkID, snapshotID string) (*domain.Snapshot, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := domain.ResolveNetworkID(networkID, s.session.Network())
	if err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	return s.findInListing(ctx, networkID, snapshotID)
}

func (s *Snapshots) findInListing(ctx context.Context, networkID, snapshotID string) (*domain.Snapshot, *ports.Response, error) {
	limit := int32(snapshotSearchLimit)
	includeArchived := true
	// A predicted snapshot is a snapshot too: Predict.RunOperation polls the one
	// it started through Get, and Forward 26.11's listing hides forks unless asked.
	includePredicted := true
	snapshots, resp, err := s.List(ctx, networkID, ports.SnapshotListOptions{
		Limit:            &limit,
		IncludeArchived:  &includeArchived,
		IncludePredicted: &includePredicted,
	})
	if err != nil {
		return nil, resp, err
	}
	for i := range snapshots {
		if string(snapshots[i].ID) == snapshotID {
			return &snapshots[i], resp, nil
		}
	}
	return nil, resp, fmt.Errorf("%w: %s", domain.ErrSnapshotNotFound, snapshotID)
}

// LatestProcessed returns the newest processed snapshot that records a state
// the network was actually in: Predict results, forks (a parent snapshot) and drafts are skipped, as Forward's own
// SnapshotService.getLatestProcessedSnapshot does. "Newest" is the listing's order (newest first); the rows are not re-sorted.
//
// Before Forward 26.11 the snapshot listing included predicted snapshots
// (Predict forks a snapshot and processes it like any other), so the newest
// PROCESSED row on a network that uses Predict was frequently a prediction; 26.11
// leaves them out unless IncludePredicted is set (FWD-61712), and this still
// applies the rule itself so both generations agree. The dedicated
// /latestProcessed route skipped forks, but it is deprecated for removal in
// 26.12; this reads the listing instead and applies the same rule. The listing
// has no offset, so it is read without a limit -- Forward assembles every row
// server-side either way. A caller that does want predictions included can
// take the first row of List with State "PROCESSED" and IncludePredicted set. LatestCollected is the
// same call.
//
// The rule was read from SnapshotService.getLatestProcessedSnapshot (app/.../cv/snapshot/service/SnapshotService.java) and is the same
// at the primary pin 5f7e4ec1841, the stable pin b1feb6fe1c34 and Forward master 54dac810c156: the snapshot metas newest first, without
// drafts and forks (!isDraft && !isFork), the first whose state is PROCESSED. SnapshotController marks GET .../snapshots/latestProcessed
// @Deprecated "TODO: Remove this endpoint (FWD-58051)" at master. Read from source, not run live.
//
// ErrNoSnapshots means the network has no processed, non-predicted snapshot.
func (s *Snapshots) LatestProcessed(ctx context.Context, networkID string) (*domain.Snapshot, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	snapshots, resp, err := s.List(ctx, networkID, ports.SnapshotListOptions{State: "PROCESSED"})
	if err != nil {
		return nil, resp, err
	}
	// The state is re-checked here as well: a build that ignores the filter
	// must not turn an in-progress snapshot into "latest processed".
	for i := range snapshots {
		if strings.EqualFold(snapshots[i].State, "PROCESSED") && !snapshots[i].Predicted() && !snapshots[i].IsDraft {
			return &snapshots[i], resp, nil
		}
	}
	return nil, resp, fmt.Errorf("%w: %s has no processed snapshot outside Predict", domain.ErrNoSnapshots, networkID)
}

// ResolveID implements the consumer-safe "latest" rule: prefer a processed,
// real collected snapshot; then any processed snapshot (a prediction only on builds before 26.11, which list them by default); then the newest row.
// A concrete ID is returned unchanged without a request.
func (s *Snapshots) ResolveID(ctx context.Context, networkID, which string) (domain.Identifier, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return "", nil, err
	}
	which = strings.TrimSpace(which)
	if which != "" && !strings.EqualFold(which, "latest") {
		return domain.Identifier(which), nil, nil
	}
	limit := int32(50)
	snapshots, response, err := s.List(ctx, networkID, ports.SnapshotListOptions{Limit: &limit})
	if err != nil {
		return "", response, err
	}
	for _, snapshot := range snapshots {
		if strings.EqualFold(snapshot.State, "PROCESSED") && !snapshot.Predicted() {
			return snapshot.ID, response, nil
		}
	}
	for _, snapshot := range snapshots {
		if strings.EqualFold(snapshot.State, "PROCESSED") {
			return snapshot.ID, response, nil
		}
	}
	if len(snapshots) != 0 {
		return snapshots[0].ID, response, nil
	}
	return "", response, domain.ErrNoSnapshots
}

// Collect starts a network collection and returns the snapshot it produced.
//
// Collection is started through the collector-task queue, not by posting to
// the snapshots route -- that route takes a multipart upload, and answers 415
// to anything else. See Upload.
//
// The snapshot does not exist until the task finishes, so this waits for the
// task and then resolves it. The snapshot returned is not processed yet;
// Operation waits for that.
func (s *Snapshots) Collect(ctx context.Context, networkID string) (*domain.Snapshot, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := domain.ResolveNetworkID(networkID, s.session.Network())
	if err != nil {
		return nil, nil, err
	}
	poller, resp, err := s.collectorTasks.StartOperation(ctx, networkID)
	if err != nil {
		return nil, resp, err
	}
	task, resp, err := poller.Wait(ctx, ports.PollOptions[domain.CollectorTask]{Interval: 2 * time.Second})
	if err != nil {
		return nil, resp, err
	}
	snapshot, resp, err := s.ForCollectionTask(ctx, networkID, string(task.ID))
	return snapshot, resp, err
}

// ForCollectionTask returns the snapshot a collector task produced.
func (s *Snapshots) ForCollectionTask(
	ctx context.Context,
	networkID string,
	taskID string,
) (*domain.Snapshot, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	if taskID = strings.TrimSpace(taskID); taskID == "" {
		return nil, nil, errors.New("forward: collector task ID is required")
	}
	limit := int32(snapshotSearchLimit)
	snapshots, resp, err := s.List(ctx, networkID, ports.SnapshotListOptions{Limit: &limit})
	if err != nil {
		return nil, resp, err
	}
	want := strings.TrimLeft(taskID, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
	for i := range snapshots {
		recorded := string(snapshots[i].CollectionTaskID)
		if recorded != "" && (recorded == taskID || recorded == want) {
			return &snapshots[i], resp, nil
		}
	}
	return nil, resp, fmt.Errorf("%w: no snapshot for collector task %s", domain.ErrSnapshotNotFound, taskID)
}

// LatestCollected returns the most recent processed snapshot that came from a
// collection rather than a prediction.
//
// Predict refuses to run against predicted input, and every prediction leaves
// a snapshot behind, so "the newest snapshot" is usually the wrong baseline
// for a change set. This is the right one. It is LatestProcessed under the
// name a Predict caller looks for; it used to read only the newest 50 rows of
// any state, so 50 recent predictions or unprocessed snapshots hid a good
// baseline behind ErrNoSnapshots.
func (s *Snapshots) LatestCollected(ctx context.Context, networkID string) (*domain.Snapshot, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	return s.LatestProcessed(ctx, networkID)
}

// Operation returns a handle that polls a snapshot until processing reaches a
// terminal state, resolving to an error for any terminal state but PROCESSED.
func (s *Snapshots) Operation(
	ctx context.Context,
	networkID string,
	snapshotID string,
) (*Poller[domain.Snapshot], *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	snapshot, resp, err := s.Get(ctx, networkID, snapshotID)
	if err != nil {
		return nil, resp, err
	}
	poller, err := NewPoller(snapshot, func(ctx context.Context) (*domain.Snapshot, *ports.Response, error) {
		return s.Get(ctx, networkID, snapshotID)
	}, snapshotProcessingDone)
	return poller, resp, err
}
