package usecases

import (
	"context"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
)

// RunOperation starts Predict and returns a handle that polls the predicted
// snapshot metadata route until processing reaches a terminal state.
func (s *Predict) RunOperation(
	ctx context.Context,
	networkID string,
	changeSetID string,
	note string,
) (*Poller[domain.Snapshot], *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	predicted, response, err := s.Run(ctx, networkID, changeSetID, note)
	if err != nil {
		return nil, response, err
	}
	initial := &domain.Snapshot{ID: predicted.ID, State: predicted.State, CreatedAt: predicted.CreatedAt, ProcessedAt: predicted.ProcessedAt, ProcessingTrigger: predicted.ProcessingTrigger, Note: predicted.Note}
	poller, err := NewPoller(initial, func(ctx context.Context) (*domain.Snapshot, *ports.Response, error) {
		return s.snapshots.Get(ctx, networkID, string(predicted.ID))
	}, snapshotProcessingDone)
	return poller, response, err
}

func snapshotProcessingDone(snapshot *domain.Snapshot) (bool, error) {
	switch strings.ToUpper(snapshot.State) {
	case "PROCESSED":
		return true, nil
	case "FAILED", "CANCELED", "TIMED_OUT", "RESTORE_FAILED":
		return true, fmt.Errorf("forward: snapshot %s ended with state %s", snapshot.ID, snapshot.State)
	default:
		return false, nil
	}
}
