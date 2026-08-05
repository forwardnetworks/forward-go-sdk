package forward

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCollectionOperationWaitsForNewProcessedSnapshot(t *testing.T) {
	t.Parallel()

	var taskPolls atomic.Int32
	var snapshotLists atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/networks/network-1/snapshots":
			switch snapshotLists.Add(1) {
			case 1:
				_, _ = io.WriteString(w, `{"snapshots":[{"id":"old","state":"PROCESSED","processingTrigger":"COLLECTION"}]}`)
			case 2:
				// A concurrent import must not satisfy a collection operation.
				_, _ = io.WriteString(w, `{"snapshots":[{"id":"imported","state":"PROCESSED","processingTrigger":"IMPORT"},{"id":"old","state":"PROCESSED","processingTrigger":"COLLECTION"}]}`)
			case 3:
				_, _ = io.WriteString(w, `{"snapshots":[{"id":"new","state":"PROCESSING","processingTrigger":"COLLECTION"},{"id":"imported","state":"PROCESSED","processingTrigger":"IMPORT"},{"id":"old","state":"PROCESSED","processingTrigger":"COLLECTION"}]}`)
			case 4:
				// One successful omission is tolerated after the snapshot appeared.
				_, _ = io.WriteString(w, `{"snapshots":[]}`)
			default:
				_, _ = io.WriteString(w, `{"snapshots":[{"id":"new","state":"PROCESSED","processingTrigger":"COLLECTION"}]}`)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/collector-tasks":
			_, _ = io.WriteString(w, `{"taskId":"P100"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/collector-tasks/P100":
			if taskPolls.Add(1) == 1 {
				_, _ = io.WriteString(w, `{"id":"P100","networkId":"network-1","status":"RUNNING"}`)
			} else {
				_, _ = io.WriteString(w, `{"id":"P100","networkId":"network-1","status":"SUCCEEDED"}`)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	operation, _, err := client.CollectorTasks.StartCollectionOperation(context.Background(), "network-1", CollectionStartOptions{})
	if err != nil {
		t.Fatalf("StartCollectionOperation() error = %v", err)
	}
	if got := operation.Handle().BaselineSnapshotID; got != "old" {
		t.Fatalf("baseline snapshot = %q", got)
	}

	var phases []OperationPhase
	snapshot, err := operation.Wait(context.Background(), CollectionWaitOptions{
		PollInterval:              time.Millisecond,
		SnapshotAppearanceTimeout: time.Second,
		SnapshotProcessingTimeout: time.Second,
		OnUpdate: func(update CollectionOperationUpdate) {
			phases = append(phases, update.Current.Phase)
		},
	})
	if err != nil || snapshot == nil || snapshot.ID != "new" || snapshot.State != "PROCESSED" {
		t.Fatalf("Wait() = %#v, %v", snapshot, err)
	}
	if operation.Handle().SnapshotID != "new" || operation.Current().Phase != OperationPhaseSucceeded {
		t.Fatalf("final state = %#v", operation.Current())
	}
	if !containsPhase(phases, OperationPhaseCollecting) || !containsPhase(phases, OperationPhaseWaitingForSnapshot) ||
		!containsPhase(phases, OperationPhaseProcessingSnapshot) || !containsPhase(phases, OperationPhaseSucceeded) {
		t.Fatalf("callback phases = %#v", phases)
	}

	encoded, err := json.Marshal(operation.Handle())
	if err != nil {
		t.Fatal(err)
	}
	var durable CollectionOperationHandle
	if err := json.Unmarshal(encoded, &durable); err != nil {
		t.Fatal(err)
	}
	resumed, err := client.CollectorTasks.ResumeCollectionOperation(durable)
	if err != nil || resumed.Handle().SnapshotID != "new" {
		t.Fatalf("ResumeCollectionOperation() = %#v, %v", resumed, err)
	}
}

func TestCollectionOperationTerminalAndDisappearanceErrors(t *testing.T) {
	t.Parallel()

	t.Run("canceled task", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"id":"P200","networkId":"network-1","status":"CANCELED"}`)
		}))
		defer server.Close()
		client := newTestClient(t, server.URL)
		operation, err := client.CollectorTasks.ResumeCollectionOperation(CollectionOperationHandle{
			NetworkID: "network-1", TaskID: "P200", StartedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := operation.Wait(context.Background(), CollectionWaitOptions{PollInterval: time.Millisecond}); err == nil || !strings.Contains(err.Error(), "CANCELED") {
			t.Fatalf("Wait() error = %v", err)
		}
		if operation.Current().Phase != OperationPhaseFailed {
			t.Fatalf("phase = %q", operation.Current().Phase)
		}
	})

	t.Run("known snapshot disappears", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"snapshots":[]}`)
		}))
		defer server.Close()
		client := newTestClient(t, server.URL)
		operation, err := client.CollectorTasks.ResumeCollectionOperation(CollectionOperationHandle{
			NetworkID: "network-1", TaskID: "P201", SnapshotID: "missing", StartedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = operation.Wait(context.Background(), CollectionWaitOptions{
			PollInterval: time.Millisecond, SnapshotProcessingTimeout: time.Second, MissingSnapshotThreshold: 2,
		})
		if !errors.Is(err, ErrSnapshotDisappeared) {
			t.Fatalf("Wait() error = %v", err)
		}
	})
}

func TestCollectionOperationSnapshotAppearanceTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/collector-tasks/P300":
			_, _ = io.WriteString(w, `{"id":"P300","networkId":"network-1","status":"SUCCEEDED"}`)
		case "/api/networks/network-1/snapshots":
			_, _ = io.WriteString(w, `{"snapshots":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	operation, err := client.CollectorTasks.ResumeCollectionOperation(CollectionOperationHandle{
		NetworkID: "network-1", TaskID: "P300", StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = operation.Wait(context.Background(), CollectionWaitOptions{
		PollInterval: time.Millisecond, SnapshotAppearanceTimeout: 3 * time.Millisecond,
	})
	if !errors.Is(err, ErrSnapshotAppearanceTimeout) {
		t.Fatalf("Wait() error = %v", err)
	}
}

func TestCollectionOperationCorrelatesWhenBaselineWasDeleted(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2099, time.August, 5, 12, 0, 0, 0, time.UTC)
	var lists atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/collector-tasks/P400":
			_, _ = io.WriteString(w, `{"id":"P400","networkId":"network-1","status":"SUCCEEDED","createdAt":"2099-08-05T12:00:00Z","finishedAt":"2099-08-05T12:00:01Z"}`)
		case "/api/networks/network-1/snapshots":
			if lists.Add(1) == 1 {
				// The baseline is gone; this older collection must not be selected.
				_, _ = io.WriteString(w, `{"snapshots":[{"id":"older","state":"PROCESSED","processingTrigger":"COLLECTION","createdAt":"2099-08-05T11:00:00Z"}]}`)
			} else {
				_, _ = io.WriteString(w, `{"snapshots":[{"id":"current","state":"PROCESSED","processingTrigger":"COLLECTION","createdAt":"2099-08-05T12:00:02Z"},{"id":"older","state":"PROCESSED","processingTrigger":"COLLECTION","createdAt":"2099-08-05T11:00:00Z"}]}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	operation, err := client.CollectorTasks.ResumeCollectionOperation(CollectionOperationHandle{
		NetworkID: "network-1", TaskID: "P400", BaselineSnapshotID: "deleted", StartedAt: startedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := operation.Wait(context.Background(), CollectionWaitOptions{
		PollInterval: time.Millisecond, SnapshotAppearanceTimeout: time.Second,
	})
	if err != nil || snapshot == nil || snapshot.ID != "current" {
		t.Fatalf("Wait() = %#v, %v", snapshot, err)
	}
}

func TestUploadOperationForcesAsyncAndResumes(t *testing.T) {
	t.Parallel()

	var lists atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/network-1/snapshots":
			if r.URL.Query().Get("async") != "true" {
				t.Errorf("async = %q", r.URL.Query().Get("async"))
			}
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"id":"uploaded","state":"PROCESSING","processingTrigger":"IMPORT"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/networks/network-1/snapshots":
			if lists.Add(1) == 1 {
				_, _ = io.WriteString(w, `{"snapshots":[]}`)
			} else {
				_, _ = io.WriteString(w, `{"snapshots":[{"id":"uploaded","state":"PROCESSED","processingTrigger":"IMPORT"}]}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	operation, _, err := client.Snapshots.StartUploadOperation(
		context.Background(),
		"network-1",
		[]SnapshotUploadFile{{Name: "snapshot.zip", Reader: strings.NewReader("zip")}},
		SnapshotUploadOptions{},
	)
	if err != nil {
		t.Fatalf("StartUploadOperation() error = %v", err)
	}
	snapshot, err := operation.Wait(context.Background(), SnapshotWaitOptions{
		PollInterval: time.Millisecond, ProcessingTimeout: time.Second, MissingSnapshotThreshold: 2,
	})
	if err != nil || snapshot == nil || snapshot.State != "PROCESSED" {
		t.Fatalf("Wait() = %#v, %v", snapshot, err)
	}
	resumed, err := client.Snapshots.ResumeOperation(operation.Handle())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = resumed.Wait(context.Background(), SnapshotWaitOptions{PollInterval: time.Millisecond})
	if err != nil || snapshot == nil || snapshot.ID != "uploaded" {
		t.Fatalf("resumed Wait() = %#v, %v", snapshot, err)
	}
}

func TestSnapshotOperationHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"snapshots":[]}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	operation, err := client.Snapshots.ResumeOperation(SnapshotOperationHandle{NetworkID: "network-1", SnapshotID: "future"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := operation.Wait(ctx, SnapshotWaitOptions{PollInterval: time.Second}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() error = %v", err)
	}
}

func TestSnapshotOperationProcessingDeadlineSurvivesResume(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"snapshots":[{"id":"slow","state":"PROCESSING"}]}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	old := time.Now().Add(-time.Hour).UTC()
	operation, err := client.Snapshots.ResumeOperation(SnapshotOperationHandle{
		NetworkID: "network-1", SnapshotID: "slow", StartedAt: old, AppearedAt: old,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = operation.Wait(context.Background(), SnapshotWaitOptions{
		PollInterval: time.Millisecond, ProcessingTimeout: time.Minute,
	})
	if !errors.Is(err, ErrSnapshotProcessingTimeout) {
		t.Fatalf("Wait() error = %v", err)
	}
}

func containsPhase(phases []OperationPhase, phase OperationPhase) bool {
	for _, value := range phases {
		if value == phase {
			return true
		}
	}
	return false
}
