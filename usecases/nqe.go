package usecases

import (
	"context"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
)

// StartOperation submits an asynchronous NQE query and returns a stateful
// polling handle.
func (s *NQE) StartOperation(
	ctx context.Context,
	networkID string,
	snapshotID string,
	query domain.NQEExecutionRequest,
) (*Poller[domain.NQEExecution], *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	execution, response, err := s.Start(ctx, networkID, snapshotID, query)
	if err != nil {
		return nil, response, err
	}
	poller, err := NewPoller(execution, func(ctx context.Context) (*domain.NQEExecution, *ports.Response, error) {
		return s.Status(ctx, networkID, execution.ExecutionKey)
	}, func(value *domain.NQEExecution) (bool, error) {
		switch strings.ToUpper(value.Status) {
		case "COMPLETED":
			if value.Outcome == "" || strings.EqualFold(value.Outcome, "OK") || strings.EqualFold(value.Outcome, "SUCCEEDED") {
				return true, nil
			}
			return true, fmt.Errorf("forward: NQE execution %s completed with outcome %s", value.ExecutionKey, value.Outcome)
		case "FAILED", "CANCELED", "TIMED_OUT":
			return true, fmt.Errorf("forward: NQE execution %s ended with status %s", value.ExecutionKey, value.Status)
		default:
			return false, nil
		}
	})
	return poller, response, err
}
