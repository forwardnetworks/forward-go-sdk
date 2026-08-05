package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPredictRoutesAndBodies(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/network-1/change-sets" && r.URL.RawQuery == "":
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			if len(body) != 4 || string(body["snapshotId"]) != `"snapshot-1"` {
				t.Errorf("create body = %#v", body)
			}
			_, _ = io.WriteString(w, `{"id":42,"name":"upgrade","networkId":"network-1","snapshotId":"snapshot-1"}`)
		case r.Method == http.MethodPut && r.URL.EscapedPath() == "/api/networks/network-1/change-sets/42/draft/devices/edge%2F1/commands":
			if got := r.Header.Get("Content-Type"); got != "text/plain;charset=UTF-8" {
				t.Errorf("Content-Type = %q", got)
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != "router bgp 65001" {
				t.Errorf("commands = %q", body)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/network-1/change-sets/42":
			if r.URL.Query().Get("action") != "predict" || r.URL.Query().Get("note") != "SDK test" {
				t.Errorf("predict query = %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"id":"predicted-1","state":"PROCESSING"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/networks/network-1/change-sets/42/predicted-snapshots":
			_, _ = io.WriteString(w, `[{"id":"predicted-1","state":"PROCESSED"}]`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()
	changeSet, _, err := client.Predict.CreateChangeSet(ctx, "network-1", ChangeSetCreateRequest{
		Name: "upgrade", Description: "IOS upgrade", SnapshotID: "snapshot-1", Tags: []string{"maintenance"},
	})
	if err != nil || changeSet.ID != "42" {
		t.Fatalf("CreateChangeSet() = %#v, %v", changeSet, err)
	}
	if _, err := client.Predict.StageCommands(ctx, "network-1", "42", "edge/1", "router bgp 65001"); err != nil {
		t.Fatalf("StageCommands() error = %v", err)
	}
	predicted, _, err := client.Predict.Run(ctx, "network-1", "42", "SDK test")
	if err != nil || predicted.ID != "predicted-1" {
		t.Fatalf("Run() = %#v, %v", predicted, err)
	}
	history, _, err := client.Predict.ListPredictedSnapshots(ctx, "network-1", "42")
	if err != nil || len(history) != 1 || history[0].State != "PROCESSED" {
		t.Fatalf("ListPredictedSnapshots() = %#v, %v", history, err)
	}
}

func TestAIChatRoutes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/ai-chats":
			if r.URL.Query().Get("networkId") != "network-1" || r.URL.Query().Get("snapshotId") != "snapshot-1" {
				t.Errorf("start query = %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"id":7,"networkId":"network-1","snapshotId":"snapshot-1","status":"PROCESSING"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/ai-chats/7/messages":
			if r.URL.Query().Get("networkId") != "network-1" {
				t.Errorf("message query = %s", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodGet && r.URL.Path == "/api/ai-chats/7/messages":
			if r.URL.Query().Get("since") == "" {
				t.Error("since is missing")
			}
			_, _ = io.WriteString(w, `{"messages":[{"id":8,"prompt":"next","finalAnswer":{"answer":"done on this build"}}]}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()
	chat, _, err := client.AI.StartChat(ctx, "network-1", "snapshot-1", "find risks")
	if err != nil || chat.ID != "7" {
		t.Fatalf("StartChat() = %#v, %v", chat, err)
	}
	if _, err := client.AI.AddMessage(ctx, "network-1", "7", "next"); err != nil {
		t.Fatalf("AddMessage() error = %v", err)
	}
	since := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	messages, _, err := client.AI.ListMessages(ctx, "7", AIMessageListOptions{Since: &since})
	if err != nil || len(messages) != 1 || messages[0].FinalAnswer.AnswerText() != "done on this build" {
		t.Fatalf("ListMessages() = %#v, %v", messages, err)
	}
}

func TestAIAssistRoutes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/nqe/query-assists":
			_, _ = io.WriteString(w, `{"id":"assist-1","query":"foreach d in network.devices select d.name","validQuery":true}`)
		case "/api/networks/network-1/change-sets/change-1/devices/edge/1/cli-assists":
			_, _ = io.WriteString(w, `{"commands":"interface Ethernet1"}`)
		case "/api/diffs/before/after/impact-summary-assists":
			_, _ = io.WriteString(w, `{"summary":"No reachability impact"}`)
		default:
			t.Errorf("unexpected assist route: %s", r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()
	assist, _, err := client.AIAssist.GenerateNQEQuery(ctx, "show device names")
	if err != nil || !assist.ValidQuery {
		t.Fatalf("GenerateNQEQuery() = %#v, %v", assist, err)
	}
	commands, _, err := client.AIAssist.GeneratePredictCLI(ctx, "network-1", "change-1", "edge/1", "enable port")
	if err != nil || commands != "interface Ethernet1" {
		t.Fatalf("GeneratePredictCLI() = %q, %v", commands, err)
	}
	summary, _, err := client.AIAssist.SummarizeChangeImpact(ctx, "before", "after")
	if err != nil || summary != "No reachability impact" {
		t.Fatalf("SummarizeChangeImpact() = %q, %v", summary, err)
	}
}
