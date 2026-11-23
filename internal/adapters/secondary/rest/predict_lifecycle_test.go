package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A change set is edited in place (metadata, config, firewall rules), versioned
// by commits, and copied; each call is a distinct Forward route with its own
// body, so the wire is asserted exactly, including that nil patch fields send
// nothing and that nothing reaches Forward for input it would refuse.
func TestPredictLifecycleWire(t *testing.T) {
	t.Parallel()

	type call struct{ line, body, contentType string }
	var calls []call
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, call{r.Method + " " + r.URL.RequestURI(), string(body), r.Header.Get("Content-Type")})
		// The three text routes produce text/plain only: a JSON-only Accept is a 406 from a real Forward.
		wantsText := (r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/config")) || r.URL.RawQuery == "action=preview"
		if wantsText && !strings.Contains(r.Header.Get("Accept"), "text/plain") {
			t.Errorf("%s %s sent Accept %q, which Forward answers with 406", r.Method, r.URL.Path, r.Header.Get("Accept"))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/config") && r.Method == http.MethodGet, strings.HasSuffix(r.URL.RawQuery, "action=preview"):
			_, _ = w.Write([]byte("hostname r1\n"))
		case strings.HasSuffix(r.URL.Path, "/commits") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[{"id":"cs1","name":"x","snapshotId":"s1","commitId":"c2","note":"v2","committedById":"7","committedAt":"2026-10-17T00:00:00Z"},{"id":"cs1","name":"x","snapshotId":"s1","commitId":"c1","note":"v1"}]`))
		case strings.HasSuffix(r.URL.Path, "/draft") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"draft":{"id":"cs1","name":"x","savedAt":1790859432171,"deviceToChanges":{"r1":{}}}}`))
		case r.Method == http.MethodPut || r.Method == http.MethodDelete || strings.Contains(r.URL.Path, "security-rules"):
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = w.Write([]byte(`{"id":"cs2","name":"copy","commitId":"c3"}`))
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	p := client.Predict
	ctx := context.Background()
	name := "renamed"

	if _, _, err := p.UpdateChangeSet(ctx, "5", "cs1", ports.ChangeSetPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.DuplicateChangeSet(ctx, "5", "cs1", ports.DuplicateChangeSetOptions{TargetName: "copy"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.DuplicateChangeSetToNetwork(ctx, "5", "cs1", "9", ports.DuplicateChangeSetOptions{TargetBaseSnapshotID: "s9"}); err != nil {
		t.Fatal(err)
	}
	commits, _, err := p.ListCommits(ctx, "5", "cs1")
	if err != nil || len(commits) != 2 || commits[0].CommitID != "c2" || commits[0].Note != "v2" || commits[0].CommittedByID != "7" {
		t.Fatalf("commits: %v %+v", err, commits)
	}
	draft, _, err := p.GetDraft(ctx, "5", "cs1")
	if err != nil || draft.Draft == nil || draft.Draft.SavedAt.String() != "1790859432171" || len(draft.Draft.DeviceToChanges) != 1 {
		t.Fatalf("draft: %v %+v", err, draft)
	}
	if _, _, err := p.RestoreCommit(ctx, "5", "cs1", "c1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.RestoreCommitAsNew(ctx, "5", "cs1", "c1", "again", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.EditCommitNote(ctx, "5", "cs1", "c1", "why a change"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.DeleteCommit(ctx, "5", "cs1", "c1"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SaveDeviceConfig(ctx, "5", "cs1", "r1", "hostname r1\n!\n"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := p.GetDraftConfig(ctx, "5", "cs1", "r1")
	if err != nil || cfg != "hostname r1\n" {
		t.Fatalf("draft config: %v %q", err, cfg)
	}
	if _, _, err := p.GetCommittedConfig(ctx, "5", "cs1", "c1", "r1"); err != nil {
		t.Fatal(err)
	}
	preview, _, err := p.PreviewCommandsConfig(ctx, "5", "cs1", "r1", "interface Gi1\n description x")
	if err != nil || preview != "hostname r1\n" {
		t.Fatalf("preview: %v %q", err, preview)
	}
	rule := ports.SecurityRuleDefinition{Name: "allow-web", Action: ports.SecurityActionPermit}
	if _, err := p.EditSecurityRule(ctx, "5", "cs1", "fw1", ports.FirewallScopeLocal, ports.FirewallRulebasePrimary, "u1", rule); err != nil {
		t.Fatal(err)
	}
	if _, err := p.MoveSecurityRule(ctx, "5", "cs1", "fw1", ports.FirewallScopeLocal, ports.FirewallRulebasePrimary, "u1", "u0"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.MoveSecurityRule(ctx, "5", "cs1", "fw1", ports.FirewallScopeLocal, ports.FirewallRulebasePrimary, "u1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := p.DiscardSecurityRuleChange(ctx, "5", "cs1", "fw1", ports.FirewallScopeLocal, ports.FirewallRulebasePrimary, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.DiscardDraftDevice(ctx, "5", "cs1", "r1"); err != nil {
		t.Fatal(err)
	}

	rb := "/api/networks/5/change-sets/cs1/devices/fw1/scopes/LOCAL/rulebases/PRIMARY/security-rules/u1"
	want := []call{
		{"PATCH /api/networks/5/change-sets/cs1", `{"name":"renamed"}`, "application/json"},
		{"POST /api/networks/5/change-sets/cs1?action=duplicate", `{"targetName":"copy"}`, "application/json"},
		{"POST /api/networks/5/change-sets/cs1?action=duplicate&targetNetworkId=9", `{"targetBaseSnapshotId":"s9"}`, "application/json"},
		{"GET /api/networks/5/change-sets/cs1/commits", "", ""},
		{"GET /api/networks/5/change-sets/cs1/draft", "", ""},
		{"POST /api/networks/5/change-sets/cs1/commits/c1?action=restore", "", ""},
		{"POST /api/networks/5/change-sets/cs1/commits/c1?action=restoreAsNew&targetName=again", "", ""},
		{"PATCH /api/networks/5/change-sets/cs1/commits/c1?note=why+a+change", "", ""},
		{"DELETE /api/networks/5/change-sets/cs1/commits/c1", "", ""},
		{"PUT /api/networks/5/change-sets/cs1/draft/devices/r1/config", "hostname r1\n!\n", "text/plain;charset=UTF-8"},
		{"GET /api/networks/5/change-sets/cs1/draft/devices/r1/config", "", ""},
		{"GET /api/networks/5/change-sets/cs1/commits/c1/devices/r1/config", "", ""},
		{"POST /api/networks/5/change-sets/cs1/devices/r1/config?action=preview", "interface Gi1\n description x", "text/plain;charset=UTF-8"},
		{"PATCH " + rb, `{"definition":{"name":"allow-web","id":"u1","description":"","enabled":false,"action":"PERMIT","sourceAddresses":{},"destinationAddresses":{},"sourceUsers":{},"applications":{},"services":{}}}`, "application/json"},
		{"PATCH " + rb, `{"predecessor":"u0"}`, "application/json"},
		{"PATCH " + rb, `{"predecessor":null}`, "application/json"},
		{"POST " + rb + "?action=discardChanges", "", ""},
		{"DELETE /api/networks/5/change-sets/cs1/draft/devices/r1", "", ""},
	}
	if len(calls) != len(want) {
		t.Fatalf("%d calls, want %d: %+v", len(calls), len(want), calls)
	}
	for i := range want {
		got := calls[i]
		if got.line != want[i].line || got.body != want[i].body || got.contentType != want[i].contentType {
			t.Errorf("call %d = %+v, want %+v", i, got, want[i])
		}
	}

	sent := len(calls)
	for name, call := range map[string]func() error{
		"blank config": func() error { _, err := p.SaveDeviceConfig(ctx, "5", "cs1", "r1", " \n"); return err },
		"blank note":   func() error { _, _, err := p.EditCommitNote(ctx, "5", "cs1", "c1", " "); return err },
		"no commit":    func() error { _, _, err := p.GetCommit(ctx, "5", "cs1", " "); return err },
		"no rule id": func() error {
			_, err := p.MoveSecurityRule(ctx, "5", "cs1", "fw1", "LOCAL", "PRIMARY", " ", "u0")
			return err
		},
		"nameless": func() error {
			_, err := p.EditSecurityRule(ctx, "5", "cs1", "fw1", "LOCAL", "PRIMARY", "u1", ports.SecurityRuleDefinition{})
			return err
		},
		"no target": func() error {
			_, _, err := p.DuplicateChangeSetToNetwork(ctx, "5", "cs1", " ", ports.DuplicateChangeSetOptions{})
			return err
		},
		"no target snapshot": func() error {
			_, _, err := p.DuplicateChangeSetToNetwork(ctx, "5", "cs1", "9", ports.DuplicateChangeSetOptions{})
			return err
		},
		"huge config": func() error {
			_, err := p.SaveDeviceConfig(ctx, "5", "cs1", "r1", strings.Repeat("x", maxDeviceConfigBytes+1))
			return err
		},
		"blank preview": func() error {
			_, _, err := p.PreviewCommandsConfig(ctx, "5", "cs1", "r1", "  \n")
			return err
		},
		"dot commit": func() error { _, _, err := p.GetCommit(ctx, "5", "cs1", ".."); return err },
	} {
		if call() == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if len(calls) != sent {
		t.Fatal("a refused call reached Forward")
	}
}
