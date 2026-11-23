package domain

// SnapshotProcessingQueueEntry is one snapshot waiting to be processed (cv.web.json.SnapshotProcessingQueueEntry): its id, when the
// request was created (an ISO-8601 time), and whether it is a REprocessing of an already processed snapshot.
type SnapshotProcessingQueueEntry struct {
	SnapshotID     Identifier        `json:"snapshotId"`
	CreatedAt      FlexibleTimestamp `json:"createdAt"`
	IsReprocessing bool              `json:"isReprocessing"`
}
