package forward

import (
	"context"
	"net/http"
)

// SnapshotProcessingQueueEntry is one snapshot waiting to be processed (cv.web.json.SnapshotProcessingQueueEntry): its id, when the
// request was created (an ISO-8601 time), and whether it is a REprocessing of an already processed snapshot.
type SnapshotProcessingQueueEntry struct {
	SnapshotID     Identifier        `json:"snapshotId"`
	CreatedAt      FlexibleTimestamp `json:"createdAt"`
	IsReprocessing bool              `json:"isReprocessing"`
}

// ProcessingQueue lists the organization's snapshot processing queue (GET /api/snapshot-processing-queue,
// SnapshotController.getSnapshotProcessingQueue; the org operation VIEW_SNAPSHOT_QUEUE), oldest request first: the snapshots waiting
// for one of the org's in-progress processing slots. It is the admission queue in front of compute, not the compute queue: it holds
// only snapshot processing, reprocessing and import (never Predict, NQE or search tasks), and a snapshot that leaves it can still wait
// in the job scheduler. It is always empty when the deployment has THROTTLE_SNAPSHOT_PROCESSING off, so an empty list does not mean the
// org is idle. Forward has no route for the per-org compute worker cap; running and queued tasks with their worker class are only in the
// system-admin debug view, Jobs.DebugView.
func (s *SnapshotsService) ProcessingQueue(ctx context.Context) ([]SnapshotProcessingQueueEntry, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/snapshot-processing-queue", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.ProcessingQueue")
	var out struct {
		Entries []SnapshotProcessingQueueEntry `json:"entries"`
	}
	resp, err := s.client.doRequired(req, &out)
	return out.Entries, resp, err
}
