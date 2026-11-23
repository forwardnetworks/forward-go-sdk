package usecases

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
)

// StartOperation starts collection and returns a handle retaining its latest
// task state. Call Wait with a callback to drive logs, UI, or Terraform
// diagnostics.
func (s *CollectorTasks) StartOperation(ctx context.Context, networkID string) (*Poller[domain.CollectorTask], *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	taskID, response, err := s.Start(ctx, networkID)
	if err != nil {
		return nil, response, err
	}
	initial := &domain.CollectorTask{ID: domain.Identifier(taskID), Type: "NETWORK_COLLECTION", Status: domain.CollectorTaskQueued, NetworkID: domain.Identifier(strings.TrimSpace(networkID))}
	poller, err := NewPoller(initial, func(ctx context.Context) (*domain.CollectorTask, *ports.Response, error) {
		return s.Get(ctx, taskID)
	}, collectorTaskDone)
	return poller, response, err
}

func collectorTaskDone(task *domain.CollectorTask) (bool, error) {
	if task == nil {
		return false, errors.New("forward: collector task state is nil")
	}
	switch task.Status {
	case domain.CollectorTaskSucceeded:
		return true, nil
	case domain.CollectorTaskFailed, domain.CollectorTaskTimedOut, domain.CollectorTaskCanceled:
		return true, fmt.Errorf("forward: collector task %s ended with status %s", task.ID, task.Status)
	default:
		return false, nil
	}
}
