package forward

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// OperationPhase is a lifecycle phase shared by composite operations.
type OperationPhase string

const (
	OperationPhaseCollecting         OperationPhase = "COLLECTING"
	OperationPhaseWaitingForSnapshot OperationPhase = "WAITING_FOR_SNAPSHOT"
	OperationPhaseProcessingSnapshot OperationPhase = "PROCESSING_SNAPSHOT"
	OperationPhaseSucceeded          OperationPhase = "SUCCEEDED"
	OperationPhaseFailed             OperationPhase = "FAILED"
)

var (
	ErrSnapshotAppearanceTimeout = errors.New("forward: snapshot appearance timeout")
	ErrSnapshotProcessingTimeout = errors.New("forward: snapshot processing timeout")
	ErrSnapshotDisappeared       = errors.New("forward: snapshot disappeared")
)

const (
	defaultOperationPollInterval           = 2 * time.Second
	defaultSnapshotAppearanceTimeout       = 5 * time.Minute
	defaultSnapshotProcessingTimeout       = 60 * time.Minute
	defaultSnapshotMissingThreshold        = 3
	operationSnapshotListLimit       int32 = 1000
)

// CollectionOperationHandle contains everything needed to resume a collection
// after a process or worker restart. Persist this value, not CollectionOperation.
type CollectionOperationHandle struct {
	NetworkID                   string    `json:"networkId"`
	TaskID                      string    `json:"taskId"`
	BaselineSnapshotID          string    `json:"baselineSnapshotId,omitempty"`
	SnapshotID                  string    `json:"snapshotId,omitempty"`
	StartedAt                   time.Time `json:"startedAt"`
	SnapshotAppearanceStartedAt time.Time `json:"snapshotAppearanceStartedAt,omitempty"`
	SnapshotProcessingStartedAt time.Time `json:"snapshotProcessingStartedAt,omitempty"`
}

// CollectionOperationState is the latest state observed across both the
// collector task and resulting snapshot.
type CollectionOperationState struct {
	Handle   CollectionOperationHandle `json:"handle"`
	Phase    OperationPhase            `json:"phase"`
	Task     *CollectorTask            `json:"task,omitempty"`
	Snapshot *Snapshot                 `json:"snapshot,omitempty"`
}

// CollectionOperationUpdate is delivered after each successful server
// observation. Previous and Current make transition-oriented callbacks easy.
type CollectionOperationUpdate struct {
	Previous CollectionOperationState
	Current  CollectionOperationState
	Response *Response
	Attempt  int
	Elapsed  time.Duration
}

// CollectionWaitOptions controls collection-to-snapshot orchestration. A zero
// timeout selects the documented default; use the parent context for a tighter
// overall deadline.
type CollectionWaitOptions struct {
	PollInterval              time.Duration
	SnapshotAppearanceTimeout time.Duration
	SnapshotProcessingTimeout time.Duration
	MissingSnapshotThreshold  int
	OnUpdate                  func(CollectionOperationUpdate)
}

// CollectionOperation retains the current state of a collection task and the
// snapshot it creates. It may be inspected concurrently while Wait is running.
type CollectionOperation struct {
	service *CollectorTasksService

	mu     sync.RWMutex
	waitMu sync.Mutex
	state  CollectionOperationState
}

// CollectionStartOptions controls baseline capture. When BaselineSnapshotID is
// nil, the SDK reads and records the newest existing snapshot before starting
// collection. A non-nil value lets an orchestrator reuse an already observed
// baseline; point to an empty string when the network has no snapshots.
type CollectionStartOptions struct {
	BaselineSnapshotID *string
}

// StartCollectionOperation captures a pre-collection baseline and starts a
// collector task. The returned handle can be persisted immediately.
func (s *CollectorTasksService) StartCollectionOperation(
	ctx context.Context,
	networkID string,
	options CollectionStartOptions,
) (*CollectionOperation, *Response, error) {
	if s == nil || s.client == nil {
		return nil, nil, errors.New("forward: collector tasks service is nil")
	}
	networkID = strings.TrimSpace(networkID)
	if networkID == "" {
		return nil, nil, errors.New("forward: network ID is required")
	}

	baselineID := ""
	if options.BaselineSnapshotID == nil {
		limit := int32(1)
		snapshots, _, err := s.client.Snapshots.List(ctx, networkID, SnapshotListOptions{Limit: &limit})
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
	handle := CollectionOperationHandle{
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
func (s *CollectorTasksService) ResumeCollectionOperation(
	handle CollectionOperationHandle,
) (*CollectionOperation, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("forward: collector tasks service is nil")
	}
	if err := validateCollectionHandle(handle); err != nil {
		return nil, err
	}
	handle.NetworkID = strings.TrimSpace(handle.NetworkID)
	handle.TaskID = strings.TrimSpace(handle.TaskID)
	handle.BaselineSnapshotID = strings.TrimSpace(handle.BaselineSnapshotID)
	handle.SnapshotID = strings.TrimSpace(handle.SnapshotID)
	phase := OperationPhaseCollecting
	if handle.SnapshotID != "" {
		phase = OperationPhaseProcessingSnapshot
		if handle.SnapshotProcessingStartedAt.IsZero() {
			handle.SnapshotProcessingStartedAt = time.Now().UTC()
		}
	}
	return &CollectionOperation{
		service: s,
		state: CollectionOperationState{
			Handle: handle,
			Phase:  phase,
			Task: &CollectorTask{
				ID:        Identifier(handle.TaskID),
				NetworkID: Identifier(handle.NetworkID),
			},
		},
	}, nil
}

// Handle returns the current durable operation handle.
func (o *CollectionOperation) Handle() CollectionOperationHandle {
	return o.Current().Handle
}

// Current returns the latest state observed by this operation.
func (o *CollectionOperation) Current() CollectionOperationState {
	if o == nil {
		return CollectionOperationState{}
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.state
}

// Wait follows the collector task through snapshot creation and processing.
// Only a SUCCEEDED task followed by a PROCESSED snapshot returns nil error.
func (o *CollectionOperation) Wait(
	ctx context.Context,
	options CollectionWaitOptions,
) (*Snapshot, error) {
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
	if state.Phase == OperationPhaseSucceeded && state.Snapshot != nil {
		return state.Snapshot, nil
	}
	if state.Phase == OperationPhaseFailed {
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
			next.Phase = OperationPhaseCollecting
			switch task.Status {
			case CollectorTaskSucceeded:
				next.Phase = OperationPhaseWaitingForSnapshot
				if next.Handle.SnapshotAppearanceStartedAt.IsZero() {
					if finishedAt, ok := parseOperationTime(task.FinishedAt); ok {
						next.Handle.SnapshotAppearanceStartedAt = finishedAt
					} else {
						next.Handle.SnapshotAppearanceStartedAt = time.Now().UTC()
					}
				}
			case CollectorTaskFailed, CollectorTaskTimedOut, CollectorTaskCanceled:
				next.Phase = OperationPhaseFailed
			}
			o.publish(next, response, options.OnUpdate, attempt, started)
			state = next
			if state.Phase == OperationPhaseFailed {
				return nil, fmt.Errorf("forward: collector task %s ended with status %s", task.ID, task.Status)
			}
			if state.Phase == OperationPhaseWaitingForSnapshot {
				break
			}
			if err := waitOperationInterval(ctx, options.PollInterval); err != nil {
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
				if state.Phase == OperationPhaseSucceeded {
					return snapshot, nil
				}
				if state.Phase == OperationPhaseFailed {
					return snapshot, snapshotTerminalError(snapshot)
				}
				break
			}
			if !time.Now().Before(appearanceDeadline) {
				return nil, fmt.Errorf("%w: collection task %s on network %s", ErrSnapshotAppearanceTimeout, state.Handle.TaskID, state.Handle.NetworkID)
			}
			if err := waitOperationInterval(ctx, options.PollInterval); err != nil {
				return nil, err
			}
		}
	}

	return o.waitForKnownSnapshot(ctx, state, options, attempt, started)
}

func (o *CollectionOperation) waitForKnownSnapshot(
	ctx context.Context,
	state CollectionOperationState,
	options CollectionWaitOptions,
	attempt int,
	started time.Time,
) (*Snapshot, error) {
	if state.Handle.SnapshotProcessingStartedAt.IsZero() {
		state.Handle.SnapshotProcessingStartedAt = time.Now().UTC()
		o.publish(state, nil, options.OnUpdate, attempt, started)
	}
	processingDeadline := state.Handle.SnapshotProcessingStartedAt.Add(options.SnapshotProcessingTimeout)
	consecutiveMisses := 0
	for {
		snapshot, response, err := o.service.client.Snapshots.find(ctx, state.Handle.NetworkID, state.Handle.SnapshotID)
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
			case OperationPhaseSucceeded:
				return snapshot, nil
			case OperationPhaseFailed:
				return snapshot, snapshotTerminalError(snapshot)
			}
		}
		if consecutiveMisses >= options.MissingSnapshotThreshold {
			return state.Snapshot, fmt.Errorf("%w: snapshot %s on network %s after %d observations", ErrSnapshotDisappeared, state.Handle.SnapshotID, state.Handle.NetworkID, consecutiveMisses)
		}
		if !time.Now().Before(processingDeadline) {
			return state.Snapshot, fmt.Errorf("%w: snapshot %s on network %s", ErrSnapshotProcessingTimeout, state.Handle.SnapshotID, state.Handle.NetworkID)
		}
		if err := waitOperationInterval(ctx, options.PollInterval); err != nil {
			return state.Snapshot, err
		}
	}
}

func (o *CollectionOperation) discoverCollectionSnapshot(
	ctx context.Context,
	handle CollectionOperationHandle,
) (*Snapshot, *Response, error) {
	limit := operationSnapshotListLimit
	snapshots, response, err := o.service.client.Snapshots.List(ctx, handle.NetworkID, SnapshotListOptions{Limit: &limit})
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
	next CollectionOperationState,
	response *Response,
	callback func(CollectionOperationUpdate),
	attempt int,
	started time.Time,
) {
	o.mu.Lock()
	previous := o.state
	o.state = next
	o.mu.Unlock()
	if callback != nil {
		callback(CollectionOperationUpdate{
			Previous: previous,
			Current:  next,
			Response: response,
			Attempt:  attempt,
			Elapsed:  time.Since(started),
		})
	}
}

// SnapshotOperationHandle contains everything needed to resume waiting for an
// uploaded or otherwise known snapshot.
type SnapshotOperationHandle struct {
	NetworkID  string    `json:"networkId"`
	SnapshotID string    `json:"snapshotId"`
	StartedAt  time.Time `json:"startedAt"`
	AppearedAt time.Time `json:"appearedAt,omitempty"`
}

// SnapshotOperationState is the latest state of a known snapshot.
type SnapshotOperationState struct {
	Handle   SnapshotOperationHandle `json:"handle"`
	Phase    OperationPhase          `json:"phase"`
	Snapshot *Snapshot               `json:"snapshot,omitempty"`
}

type SnapshotOperationUpdate struct {
	Previous SnapshotOperationState
	Current  SnapshotOperationState
	Response *Response
	Attempt  int
	Elapsed  time.Duration
}

type SnapshotWaitOptions struct {
	PollInterval             time.Duration
	AppearanceTimeout        time.Duration
	ProcessingTimeout        time.Duration
	MissingSnapshotThreshold int
	OnUpdate                 func(SnapshotOperationUpdate)
}

// SnapshotOperation retains the latest state of a known snapshot.
type SnapshotOperation struct {
	service *SnapshotsService

	mu     sync.RWMutex
	waitMu sync.Mutex
	state  SnapshotOperationState
}

// StartUploadOperation streams an upload with async=true and returns a durable
// snapshot operation handle.
func (s *SnapshotsService) StartUploadOperation(
	ctx context.Context,
	networkID string,
	files []SnapshotUploadFile,
	options SnapshotUploadOptions,
) (*SnapshotOperation, *Response, error) {
	startedAt := time.Now().UTC()
	options.Async = true
	snapshot, response, err := s.Upload(ctx, networkID, files, options)
	if err != nil {
		return nil, response, err
	}
	if snapshot == nil || strings.TrimSpace(string(snapshot.ID)) == "" {
		return nil, response, errors.New("forward: snapshot upload returned no snapshot ID")
	}
	operation, err := s.ResumeOperation(SnapshotOperationHandle{
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
func (s *SnapshotsService) ResumeOperation(handle SnapshotOperationHandle) (*SnapshotOperation, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("forward: snapshots service is nil")
	}
	handle.NetworkID = strings.TrimSpace(handle.NetworkID)
	handle.SnapshotID = strings.TrimSpace(handle.SnapshotID)
	if handle.NetworkID == "" || handle.SnapshotID == "" {
		return nil, errors.New("forward: snapshot operation network ID and snapshot ID are required")
	}
	if handle.StartedAt.IsZero() {
		handle.StartedAt = time.Now().UTC()
	}
	phase := OperationPhaseWaitingForSnapshot
	if !handle.AppearedAt.IsZero() {
		phase = OperationPhaseProcessingSnapshot
	}
	return &SnapshotOperation{
		service: s,
		state: SnapshotOperationState{
			Handle: handle,
			Phase:  phase,
		},
	}, nil
}

func (o *SnapshotOperation) Handle() SnapshotOperationHandle {
	return o.Current().Handle
}

func (o *SnapshotOperation) Current() SnapshotOperationState {
	if o == nil {
		return SnapshotOperationState{}
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.state
}

// Wait waits for a known snapshot to appear and reach a terminal state.
func (o *SnapshotOperation) Wait(ctx context.Context, options SnapshotWaitOptions) (*Snapshot, error) {
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
	if state.Phase == OperationPhaseSucceeded && state.Snapshot != nil {
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
			case OperationPhaseSucceeded:
				return snapshot, nil
			case OperationPhaseFailed:
				return snapshot, snapshotTerminalError(snapshot)
			}
		}
		if !appeared && !time.Now().Before(appearanceDeadline) {
			return nil, fmt.Errorf("%w: snapshot %s on network %s", ErrSnapshotAppearanceTimeout, state.Handle.SnapshotID, state.Handle.NetworkID)
		}
		if appeared && consecutiveMisses >= options.MissingSnapshotThreshold {
			return state.Snapshot, fmt.Errorf("%w: snapshot %s on network %s after %d observations", ErrSnapshotDisappeared, state.Handle.SnapshotID, state.Handle.NetworkID, consecutiveMisses)
		}
		if appeared && !processingDeadline.IsZero() && !time.Now().Before(processingDeadline) {
			return state.Snapshot, fmt.Errorf("%w: snapshot %s on network %s", ErrSnapshotProcessingTimeout, state.Handle.SnapshotID, state.Handle.NetworkID)
		}
		if err := waitOperationInterval(ctx, options.PollInterval); err != nil {
			return state.Snapshot, err
		}
	}
}

func (o *SnapshotOperation) publishSnapshot(
	next SnapshotOperationState,
	response *Response,
	callback func(SnapshotOperationUpdate),
	attempt int,
	started time.Time,
) {
	o.mu.Lock()
	previous := o.state
	o.state = next
	o.mu.Unlock()
	if callback != nil {
		callback(SnapshotOperationUpdate{
			Previous: previous,
			Current:  next,
			Response: response,
			Attempt:  attempt,
			Elapsed:  time.Since(started),
		})
	}
}

func (s *SnapshotsService) find(ctx context.Context, networkID, snapshotID string) (*Snapshot, *Response, error) {
	limit := operationSnapshotListLimit
	snapshots, response, err := s.List(ctx, networkID, SnapshotListOptions{Limit: &limit})
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

func validateCollectionHandle(handle CollectionOperationHandle) error {
	if strings.TrimSpace(handle.NetworkID) == "" || strings.TrimSpace(handle.TaskID) == "" {
		return errors.New("forward: collection operation network ID and task ID are required")
	}
	if handle.StartedAt.IsZero() {
		return errors.New("forward: collection operation start time is required")
	}
	return nil
}

func snapshotOperationPhase(snapshot *Snapshot) OperationPhase {
	if snapshot == nil {
		return OperationPhaseWaitingForSnapshot
	}
	switch strings.ToUpper(strings.TrimSpace(snapshot.State)) {
	case "PROCESSED":
		return OperationPhaseSucceeded
	case "FAILED", "CANCELED", "TIMED_OUT", "RESTORE_FAILED", "ERROR", "BROKEN":
		return OperationPhaseFailed
	default:
		return OperationPhaseProcessingSnapshot
	}
}

func snapshotTerminalError(snapshot *Snapshot) error {
	return fmt.Errorf("forward: snapshot %s ended with state %s", snapshot.ID, snapshot.State)
}

func normalizeCollectionWaitOptions(options CollectionWaitOptions) CollectionWaitOptions {
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

func normalizeSnapshotWaitOptions(options SnapshotWaitOptions) SnapshotWaitOptions {
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

func waitOperationInterval(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseOperationTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
