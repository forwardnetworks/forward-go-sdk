package usecases

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
	"sync"
	"time"
)

const (
	defaultOperationPollInterval           = 2 * time.Second
	defaultSnapshotAppearanceTimeout       = 5 * time.Minute
	defaultSnapshotProcessingTimeout       = 60 * time.Minute
	defaultSnapshotMissingThreshold        = 3
	operationSnapshotListLimit       int32 = 1000
)

// CollectionOperation retains the current state of a collection task and the
// snapshot it creates. It may be inspected concurrently while Wait is running.
type CollectionOperation struct {
	service *CollectorTasks

	mu     sync.RWMutex
	waitMu sync.Mutex
	state  domain.CollectionOperationState
}

// StartCollectionOperation captures a pre-collection baseline and starts a
// collector task. The returned handle can be persisted immediately.
func (s *CollectorTasks) StartCollectionOperation(
	ctx context.Context,
	networkID string,
	options domain.CollectionStartOptions,
) (*CollectionOperation, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	if s == nil || s.session == nil {
		return nil, nil, domain.NilServiceError("collector tasks")
	}
	networkID = strings.TrimSpace(networkID)
	if networkID == "" {
		return nil, nil, errors.New("forward: network ID is required")
	}

	baselineID := ""
	if options.BaselineSnapshotID == nil {
		limit := int32(1)
		snapshots, _, err := s.snapshots.List(ctx, networkID, ports.SnapshotListOptions{Limit: &limit})
		if err != nil {
			return nil, nil, fmt.Errorf("forward: capture collection snapshot baseline: %w", err)
		}
		if len(snapshots) != 0 {
			baselineID = string(snapshots[0].ID)
		}
	} else {
		baselineID = strings.TrimSpace(*options.BaselineSnapshotID)
	}

	startedAt := time.Now().UTC()
	taskID, response, err := s.Start(ctx, networkID)
	if err != nil {
		return nil, response, err
	}
	handle := domain.CollectionOperationHandle{
		NetworkID:          networkID,
		TaskID:             taskID,
		BaselineSnapshotID: baselineID,
		StartedAt:          startedAt,
	}
	operation, err := s.ResumeCollectionOperation(handle)
	return operation, response, err
}

// ResumeCollectionOperation reconstructs an operation without making a
// request. Wait will reconcile its durable identifiers with current state.
func (s *CollectorTasks) ResumeCollectionOperation(
	handle domain.CollectionOperationHandle,
) (*CollectionOperation, error) {
	if err := s.failClosed(); err != nil {
		return nil, err
	}
	if s == nil || s.session == nil {
		return nil, domain.NilServiceError("collector tasks")
	}
	if err := validateCollectionHandle(handle); err != nil {
		return nil, err
	}
	handle.NetworkID = strings.TrimSpace(handle.NetworkID)
	handle.TaskID = strings.TrimSpace(handle.TaskID)
	handle.BaselineSnapshotID = strings.TrimSpace(handle.BaselineSnapshotID)
	handle.SnapshotID = strings.TrimSpace(handle.SnapshotID)
	phase := domain.OperationPhaseCollecting
	if handle.SnapshotID != "" {
		phase = domain.OperationPhaseProcessingSnapshot
		if handle.SnapshotProcessingStartedAt.IsZero() {
			handle.SnapshotProcessingStartedAt = time.Now().UTC()
		}
	}
	return &CollectionOperation{
		service: s,
		state: domain.CollectionOperationState{
			Handle: handle,
			Phase:  phase,
			Task: &domain.CollectorTask{
				ID:        domain.Identifier(handle.TaskID),
				NetworkID: domain.Identifier(handle.NetworkID),
			},
		},
	}, nil
}

// Handle returns the current durable operation handle.
func (o *CollectionOperation) Handle() domain.CollectionOperationHandle {
	return o.Current().Handle
}

// Current returns the latest state observed by this operation.
func (o *CollectionOperation) Current() domain.CollectionOperationState {
	if o == nil {
		return domain.CollectionOperationState{}
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.state
}

// Wait follows the collector task through snapshot creation and processing.
// Only a SUCCEEDED task followed by a PROCESSED snapshot returns nil error.
func (o *CollectionOperation) Wait(
	ctx context.Context,
	options ports.CollectionWaitOptions,
) (*domain.Snapshot, error) {
	if o == nil || o.service == nil {
		return nil, errors.New("forward: collection operation is nil")
	}
	if ctx == nil {
		return nil, errors.New("forward: context is nil")
	}
	o.waitMu.Lock()
	defer o.waitMu.Unlock()

	options = normalizeCollectionWaitOptions(options)
	started := time.Now()
	attempt := 0

	state := o.Current()
	if state.Phase == domain.OperationPhaseSucceeded && state.Snapshot != nil {
		return state.Snapshot, nil
	}
	if state.Phase == domain.OperationPhaseFailed {
		return state.Snapshot, errors.New("forward: collection operation is already failed")
	}

	if state.Handle.SnapshotID == "" {
		for {
			task, response, err := o.service.Get(ctx, state.Handle.TaskID)
			if err != nil {
				return nil, err
			}
			attempt++
			next := state
			next.Task = task
			if taskTime, ok := parseOperationTime(task.CreatedAt); ok {
				next.Handle.StartedAt = taskTime
			}
			next.Phase = domain.OperationPhaseCollecting
			switch task.Status {
			case domain.CollectorTaskSucceeded:
				next.Phase = domain.OperationPhaseWaitingForSnapshot
				if next.Handle.SnapshotAppearanceStartedAt.IsZero() {
					if finishedAt, ok := parseOperationTime(task.FinishedAt); ok {
						next.Handle.SnapshotAppearanceStartedAt = finishedAt
					} else {
						next.Handle.SnapshotAppearanceStartedAt = time.Now().UTC()
					}
				}
			case domain.CollectorTaskFailed, domain.CollectorTaskTimedOut, domain.CollectorTaskCanceled:
				next.Phase = domain.OperationPhaseFailed
			}
			o.publish(next, response, options.OnUpdate, attempt, started)
			state = next
			if state.Phase == domain.OperationPhaseFailed {
				return nil, fmt.Errorf("forward: collector task %s ended with status %s", task.ID, task.Status)
			}
			if state.Phase == domain.OperationPhaseWaitingForSnapshot {
				break
			}
			if err := domain.WaitInterval(ctx, options.PollInterval); err != nil {
				return nil, err
			}
		}

		appearanceDeadline := state.Handle.SnapshotAppearanceStartedAt.Add(options.SnapshotAppearanceTimeout)
		for state.Handle.SnapshotID == "" {
			snapshot, response, err := o.discoverCollectionSnapshot(ctx, state.Handle)
			if err != nil {
				return nil, err
			}
			attempt++
			next := state
			if snapshot != nil {
				next.Handle.SnapshotID = string(snapshot.ID)
				if next.Handle.SnapshotProcessingStartedAt.IsZero() {
					next.Handle.SnapshotProcessingStartedAt = time.Now().UTC()
				}
				next.Snapshot = snapshot
				next.Phase = snapshotOperationPhase(snapshot)
			}
			o.publish(next, response, options.OnUpdate, attempt, started)
			state = next
			if snapshot != nil {
				if state.Phase == domain.OperationPhaseSucceeded {
					return snapshot, nil
				}
				if state.Phase == domain.OperationPhaseFailed {
					return snapshot, snapshotTerminalError(snapshot)
				}
				break
			}
			if !time.Now().Before(appearanceDeadline) {
				return nil, fmt.Errorf("%w: collection task %s on network %s", domain.ErrSnapshotAppearanceTimeout, state.Handle.TaskID, state.Handle.NetworkID)
			}
			if err := domain.WaitInterval(ctx, options.PollInterval); err != nil {
				return nil, err
			}
		}
	}

	return o.waitForKnownSnapshot(ctx, state, options, attempt, started)
}

func (o *CollectionOperation) waitForKnownSnapshot(
	ctx context.Context,
	state domain.CollectionOperationState,
	options ports.CollectionWaitOptions,
	attempt int,
	started time.Time,
) (*domain.Snapshot, error) {
	if state.Handle.SnapshotProcessingStartedAt.IsZero() {
		state.Handle.SnapshotProcessingStartedAt = time.Now().UTC()
		o.publish(state, nil, options.OnUpdate, attempt, started)
	}
	processingDeadline := state.Handle.SnapshotProcessingStartedAt.Add(options.SnapshotProcessingTimeout)
	consecutiveMisses := 0
	for {
		snapshot, response, err := o.service.snapshots.find(ctx, state.Handle.NetworkID, state.Handle.SnapshotID)
		if err != nil {
			return nil, err
		}
		attempt++
		next := state
		if snapshot == nil {
			consecutiveMisses++
		} else {
			consecutiveMisses = 0
			next.Snapshot = snapshot
			next.Phase = snapshotOperationPhase(snapshot)
		}
		o.publish(next, response, options.OnUpdate, attempt, started)
		state = next
		if snapshot != nil {
			switch state.Phase {
			case domain.OperationPhaseSucceeded:
				return snapshot, nil
			case domain.OperationPhaseFailed:
				return snapshot, snapshotTerminalError(snapshot)
			}
		}
		if consecutiveMisses >= options.MissingSnapshotThreshold {
			return state.Snapshot, fmt.Errorf("%w: snapshot %s on network %s after %d observations", domain.ErrSnapshotDisappeared, state.Handle.SnapshotID, state.Handle.NetworkID, consecutiveMisses)
		}
		if !time.Now().Before(processingDeadline) {
			return state.Snapshot, fmt.Errorf("%w: snapshot %s on network %s", domain.ErrSnapshotProcessingTimeout, state.Handle.SnapshotID, state.Handle.NetworkID)
		}
		if err := domain.WaitInterval(ctx, options.PollInterval); err != nil {
			return state.Snapshot, err
		}
	}
}

func (o *CollectionOperation) discoverCollectionSnapshot(
	ctx context.Context,
	handle domain.CollectionOperationHandle,
) (*domain.Snapshot, *ports.Response, error) {
	limit := operationSnapshotListLimit
	snapshots, response, err := o.service.snapshots.List(ctx, handle.NetworkID, ports.SnapshotListOptions{Limit: &limit})
	if err != nil {
		return nil, response, err
	}
	baselineIndex := -1
	for i := range snapshots {
		if string(snapshots[i].ID) == handle.BaselineSnapshotID {
			baselineIndex = i
			break
		}
	}
	searchLimit := len(snapshots)
	if baselineIndex >= 0 {
		searchLimit = baselineIndex
	}
	for i := 0; i < searchLimit; i++ {
		snapshot := &snapshots[i]
		if baselineIndex < 0 && handle.BaselineSnapshotID != "" {
			createdAt, ok := parseOperationTime(snapshot.CreatedAt)
			if !ok || createdAt.Before(handle.StartedAt) {
				continue
			}
		}
		if strings.EqualFold(snapshot.ProcessingTrigger, "COLLECTION") {
			return snapshot, response, nil
		}
	}
	return nil, response, nil
}

func (o *CollectionOperation) publish(
	next domain.CollectionOperationState,
	response *ports.Response,
	callback func(ports.CollectionOperationUpdate),
	attempt int,
	started time.Time,
) {
	o.mu.Lock()
	previous := o.state
	o.state = next
	o.mu.Unlock()
	if callback != nil {
		callback(ports.CollectionOperationUpdate{
			Previous: previous,
			Current:  next,
			Response: response,
			Attempt:  attempt,
			Elapsed:  time.Since(started),
		})
	}
}

// SnapshotOperation retains the latest state of a known snapshot.
type SnapshotOperation struct {
	service *Snapshots

	mu     sync.RWMutex
	waitMu sync.Mutex
	state  domain.SnapshotOperationState
}

// StartUploadOperation streams an upload with async=true and returns a durable
// snapshot operation handle.
func (s *Snapshots) StartUploadOperation(
	ctx context.Context,
	networkID string,
	files []domain.SnapshotUploadFile,
	options ports.SnapshotUploadOptions,
) (*SnapshotOperation, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	startedAt := time.Now().UTC()
	options.Async = true
	snapshot, response, err := s.Upload(ctx, networkID, files, options)
	if err != nil {
		return nil, response, err
	}
	if snapshot == nil || strings.TrimSpace(string(snapshot.ID)) == "" {
		return nil, response, errors.New("forward: snapshot upload returned no snapshot ID")
	}
	operation, err := s.ResumeOperation(domain.SnapshotOperationHandle{
		NetworkID:  strings.TrimSpace(networkID),
		SnapshotID: string(snapshot.ID),
		StartedAt:  startedAt,
		AppearedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, response, err
	}
	operation.state.Snapshot = snapshot
	operation.state.Phase = snapshotOperationPhase(snapshot)
	return operation, response, nil
}

// ResumeOperation reconstructs a snapshot operation without making a request.
func (s *Snapshots) ResumeOperation(handle domain.SnapshotOperationHandle) (*SnapshotOperation, error) {
	if err := s.failClosed(); err != nil {
		return nil, err
	}
	if s == nil || s.session == nil {
		return nil, domain.NilServiceError("snapshots")
	}
	handle.NetworkID = strings.TrimSpace(handle.NetworkID)
	handle.SnapshotID = strings.TrimSpace(handle.SnapshotID)
	if handle.NetworkID == "" || handle.SnapshotID == "" {
		return nil, errors.New("forward: snapshot operation network ID and snapshot ID are required")
	}
	if handle.StartedAt.IsZero() {
		handle.StartedAt = time.Now().UTC()
	}
	phase := domain.OperationPhaseWaitingForSnapshot
	if !handle.AppearedAt.IsZero() {
		phase = domain.OperationPhaseProcessingSnapshot
	}
	return &SnapshotOperation{
		service: s,
		state: domain.SnapshotOperationState{
			Handle: handle,
			Phase:  phase,
		},
	}, nil
}

func (o *SnapshotOperation) Handle() domain.SnapshotOperationHandle {
	return o.Current().Handle
}

func (o *SnapshotOperation) Current() domain.SnapshotOperationState {
	if o == nil {
		return domain.SnapshotOperationState{}
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.state
}

// Wait waits for a known snapshot to appear and reach a terminal state.
func (o *SnapshotOperation) Wait(ctx context.Context, options ports.SnapshotWaitOptions) (*domain.Snapshot, error) {
	if o == nil || o.service == nil {
		return nil, errors.New("forward: snapshot operation is nil")
	}
	if ctx == nil {
		return nil, errors.New("forward: context is nil")
	}
	o.waitMu.Lock()
	defer o.waitMu.Unlock()

	options = normalizeSnapshotWaitOptions(options)
	started := time.Now()
	attempt := 0
	state := o.Current()
	if state.Phase == domain.OperationPhaseSucceeded && state.Snapshot != nil {
		return state.Snapshot, nil
	}

	appearanceDeadline := state.Handle.StartedAt.Add(options.AppearanceTimeout)
	appeared := state.Snapshot != nil || !state.Handle.AppearedAt.IsZero()
	processingDeadline := time.Time{}
	if appeared {
		if state.Handle.AppearedAt.IsZero() {
			state.Handle.AppearedAt = time.Now().UTC()
		}
		processingDeadline = state.Handle.AppearedAt.Add(options.ProcessingTimeout)
	}
	consecutiveMisses := 0
	for {
		snapshot, response, err := o.service.find(ctx, state.Handle.NetworkID, state.Handle.SnapshotID)
		if err != nil {
			return state.Snapshot, err
		}
		attempt++
		next := state
		if snapshot != nil {
			if !appeared {
				appeared = true
				next.Handle.AppearedAt = time.Now().UTC()
				processingDeadline = next.Handle.AppearedAt.Add(options.ProcessingTimeout)
			}
			consecutiveMisses = 0
			next.Snapshot = snapshot
			next.Phase = snapshotOperationPhase(snapshot)
		} else if appeared {
			consecutiveMisses++
		}
		o.publishSnapshot(next, response, options.OnUpdate, attempt, started)
		state = next

		if snapshot != nil {
			switch state.Phase {
			case domain.OperationPhaseSucceeded:
				return snapshot, nil
			case domain.OperationPhaseFailed:
				return snapshot, snapshotTerminalError(snapshot)
			}
		}
		if !appeared && !time.Now().Before(appearanceDeadline) {
			return nil, fmt.Errorf("%w: snapshot %s on network %s", domain.ErrSnapshotAppearanceTimeout, state.Handle.SnapshotID, state.Handle.NetworkID)
		}
		if appeared && consecutiveMisses >= options.MissingSnapshotThreshold {
			return state.Snapshot, fmt.Errorf("%w: snapshot %s on network %s after %d observations", domain.ErrSnapshotDisappeared, state.Handle.SnapshotID, state.Handle.NetworkID, consecutiveMisses)
		}
		if appeared && !processingDeadline.IsZero() && !time.Now().Before(processingDeadline) {
			return state.Snapshot, fmt.Errorf("%w: snapshot %s on network %s", domain.ErrSnapshotProcessingTimeout, state.Handle.SnapshotID, state.Handle.NetworkID)
		}
		if err := domain.WaitInterval(ctx, options.PollInterval); err != nil {
			return state.Snapshot, err
		}
	}
}

func (o *SnapshotOperation) publishSnapshot(
	next domain.SnapshotOperationState,
	response *ports.Response,
	callback func(ports.SnapshotOperationUpdate),
	attempt int,
	started time.Time,
) {
	o.mu.Lock()
	previous := o.state
	o.state = next
	o.mu.Unlock()
	if callback != nil {
		callback(ports.SnapshotOperationUpdate{
			Previous: previous,
			Current:  next,
			Response: response,
			Attempt:  attempt,
			Elapsed:  time.Since(started),
		})
	}
}

func (s *Snapshots) find(ctx context.Context, networkID, snapshotID string) (*domain.Snapshot, *ports.Response, error) {
	limit := operationSnapshotListLimit
	// Looked up by ID, so a prediction counts (Forward 26.11 hides them by default).
	includePredicted := true
	snapshots, response, err := s.List(ctx, networkID, ports.SnapshotListOptions{Limit: &limit, IncludePredicted: &includePredicted})
	if err != nil {
		return nil, response, err
	}
	for i := range snapshots {
		if string(snapshots[i].ID) == snapshotID {
			return &snapshots[i], response, nil
		}
	}
	return nil, response, nil
}

func validateCollectionHandle(handle domain.CollectionOperationHandle) error {
	if strings.TrimSpace(handle.NetworkID) == "" || strings.TrimSpace(handle.TaskID) == "" {
		return errors.New("forward: collection operation network ID and task ID are required")
	}
	if handle.StartedAt.IsZero() {
		return errors.New("forward: collection operation start time is required")
	}
	return nil
}

func snapshotOperationPhase(snapshot *domain.Snapshot) domain.OperationPhase {
	if snapshot == nil {
		return domain.OperationPhaseWaitingForSnapshot
	}
	switch strings.ToUpper(strings.TrimSpace(snapshot.State)) {
	case "PROCESSED":
		return domain.OperationPhaseSucceeded
	case "FAILED", "CANCELED", "TIMED_OUT", "RESTORE_FAILED", "ERROR", "BROKEN":
		return domain.OperationPhaseFailed
	default:
		return domain.OperationPhaseProcessingSnapshot
	}
}

func snapshotTerminalError(snapshot *domain.Snapshot) error {
	return fmt.Errorf("forward: snapshot %s ended with state %s", snapshot.ID, snapshot.State)
}

func normalizeCollectionWaitOptions(options ports.CollectionWaitOptions) ports.CollectionWaitOptions {
	if options.PollInterval <= 0 {
		options.PollInterval = defaultOperationPollInterval
	}
	if options.SnapshotAppearanceTimeout <= 0 {
		options.SnapshotAppearanceTimeout = defaultSnapshotAppearanceTimeout
	}
	if options.SnapshotProcessingTimeout <= 0 {
		options.SnapshotProcessingTimeout = defaultSnapshotProcessingTimeout
	}
	if options.MissingSnapshotThreshold <= 0 {
		options.MissingSnapshotThreshold = defaultSnapshotMissingThreshold
	}
	return options
}

func normalizeSnapshotWaitOptions(options ports.SnapshotWaitOptions) ports.SnapshotWaitOptions {
	if options.PollInterval <= 0 {
		options.PollInterval = defaultOperationPollInterval
	}
	if options.AppearanceTimeout <= 0 {
		options.AppearanceTimeout = defaultSnapshotAppearanceTimeout
	}
	if options.ProcessingTimeout <= 0 {
		options.ProcessingTimeout = defaultSnapshotProcessingTimeout
	}
	if options.MissingSnapshotThreshold <= 0 {
		options.MissingSnapshotThreshold = defaultSnapshotMissingThreshold
	}
	return options
}

func parseOperationTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
