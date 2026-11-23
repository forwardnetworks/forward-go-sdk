package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// ApplyOperations calls Apply and wraps each returned collector task ID in a
// Poller, reusing CollectorTasksService.Get -- the same generic collector-task
// lookup ApplyCertificatesTask is enqueued through on the appserver
// (TrustedCertificateTaskService delegates to the same CollectorTaskService
// that backs network collection) -- instead of a second, invented poll loop.
// Callers that only need to fire-and-forget can call Apply directly and
// observe completion later (for example from a periodic reconcile pass)
// without ever constructing a Poller.
func (s *TrustedCertificates) ApplyOperations(ctx context.Context) ([]*Poller[domain.CollectorTask], *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	taskIDs, response, err := s.Apply(ctx)
	if err != nil {
		return nil, response, err
	}
	pollers := make([]*Poller[domain.CollectorTask], 0, len(taskIDs))
	for _, taskID := range taskIDs {
		id := taskID.String()
		initial := &domain.CollectorTask{ID: taskID, Type: "APPLY_CERTIFICATES", Status: domain.CollectorTaskQueued}
		poller, pollerErr := NewPoller(initial, func(ctx context.Context) (*domain.CollectorTask, *ports.Response, error) {
			return s.collectorTasks.Get(ctx, id)
		}, collectorTaskDone)
		if pollerErr != nil {
			return nil, response, pollerErr
		}
		pollers = append(pollers, poller)
	}
	return pollers, response, nil
}
