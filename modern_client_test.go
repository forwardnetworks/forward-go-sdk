package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestHooksAndRawService(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/future-resource" || r.URL.Query().Get("secretValue") != "hidden" {
			t.Errorf("request URI = %s", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"id":"future-1"}`)
	}))
	defer server.Close()

	var mu sync.Mutex
	var events []Event
	client, err := NewClient(Config{
		BaseURL: server.URL, Username: "user", Password: "pass",
		Hooks: []Hook{func(_ context.Context, event Event) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, event)
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	_, err = client.Raw.DoJSON(context.Background(), http.MethodPost, "/api/future-resource?secretValue=hidden", map[string]string{"password": "also-hidden"}, &result)
	if err != nil || result["id"] != "future-1" {
		t.Fatalf("Raw.DoJSON() = %#v, %v", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0].Type != EventRequest || events[1].StatusCode != http.StatusOK {
		t.Fatalf("events = %#v", events)
	}
	for _, event := range events {
		if event.Path != "/api/future-resource" {
			t.Errorf("hook path leaked query: %q", event.Path)
		}
	}
}

func TestPollerRetainsStateAndCallsBack(t *testing.T) {
	t.Parallel()

	type state struct{ Status string }
	states := []*state{{Status: "QUEUED"}, {Status: "RUNNING"}, {Status: "DONE"}}
	next := 1
	poller, err := NewPoller(states[0], func(context.Context) (*state, *Response, error) {
		value := states[next]
		next++
		return value, nil, nil
	}, func(value *state) (bool, error) { return value.Status == "DONE", nil })
	if err != nil {
		t.Fatal(err)
	}
	var transitions []string
	value, _, err := poller.Wait(context.Background(), PollOptions[state]{
		Interval: time.Millisecond,
		OnUpdate: func(update PollUpdate[state]) {
			transitions = append(transitions, update.Previous.Status+"->"+update.Value.Status)
		},
	})
	if err != nil || value.Status != "DONE" || poller.Current().Status != "DONE" {
		t.Fatalf("Wait() = %#v, current %#v, %v", value, poller.Current(), err)
	}
	want := []string{"QUEUED->RUNNING", "RUNNING->DONE"}
	if len(transitions) != len(want) || transitions[0] != want[0] || transitions[1] != want[1] {
		t.Fatalf("transitions = %#v", transitions)
	}
}

func TestResourceCRUDRoutes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/parent/workspaces":
			_, _ = io.WriteString(w, `{"id":"workspace-1","name":"change","orgId":"9","parentId":"parent"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/workspace-1/cli-credentials":
			var body map[string]json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, exists := body["password"]; !exists {
				t.Error("password missing")
			}
			_, _ = io.WriteString(w, `{"id":"L-3","type":"LOGIN","name":"admin","password":"secret-id"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/workspace-1/classic-devices":
			_, _ = io.WriteString(w, `{"name":"r1","host":"10.0.0.1","futureField":{"enabled":true}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/collector-tasks":
			if r.URL.Query().Get("networkId") != "workspace-1" {
				t.Errorf("networkId = %q", r.URL.Query().Get("networkId"))
			}
			_, _ = io.WriteString(w, `{"taskId":"P1234"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()
	workspace, _, err := client.Networks.CreateWorkspace(ctx, "parent", WorkspaceNetworkRequest{Name: "change", Devices: []string{"r1"}})
	if err != nil || workspace.ParentID != "parent" {
		t.Fatalf("CreateWorkspace() = %#v, %v", workspace, err)
	}
	credential, _, err := client.Credentials.CreateCLI(ctx, "workspace-1", CLICredentialRequest{Type: "LOGIN", Name: "admin", Username: "admin", Password: "password"})
	if err != nil || credential.ID != "L-3" {
		t.Fatalf("CreateCLI() = %#v, %v", credential, err)
	}
	device, _, err := client.ClassicDevices.Create(ctx, "workspace-1", ClassicDeviceRequest{Name: "r1", Host: "10.0.0.1", CLICredentialID: credential.ID})
	if err != nil || device.Name != "r1" || device.Raw["futureField"] == nil {
		t.Fatalf("ClassicDevices.Create() = %#v, %v", device, err)
	}
	taskID, _, err := client.CollectorTasks.Start(ctx, "workspace-1")
	if err != nil || taskID != "P1234" {
		t.Fatalf("CollectorTasks.Start() = %q, %v", taskID, err)
	}
}
