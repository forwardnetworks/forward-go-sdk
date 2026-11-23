package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The AutoCon6 workshop's critical flows: an Existential check for TCP 8080 between two subnets, an Isolation check for 2222, and a path
// through r2, all as the same IntentCheck body (checkType plus the GUI's path query as "filters"); an NQE check carries only a query ID.
func TestCheckDefinitionBuilders(t *testing.T) {
	q := domain.TracePathQuery{
		From: &domain.TraceEndpoint{Location: domain.TraceSubnet("10.10.10.10/32"), Headers: []domain.TraceHeaders{{"ip_proto": {"6"}, "tp_dst": {"8080"}}}},
		To:   &domain.TraceEndpoint{Location: domain.TraceSubnet("10.20.20.20/32")},
	}
	exists, err := domain.ExistentialCheckDefinition(q, domain.IntentCheckOptions{ReturnPath: "SYMMETRIC"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exists)
	body := string(raw)
	for _, want := range []string{`"checkType":"Existential"`, `"returnPath":"SYMMETRIC"`, `"type":"SubnetLocationFilter"`, `"tp_dst":["8080"]`} {
		if !strings.Contains(body, want) {
			t.Errorf("existential body lacks %s: %s", want, body)
		}
	}
	iso, err := domain.IsolationCheckDefinition(q, domain.IntentCheckOptions{})
	if err != nil || iso["checkType"] != "Isolation" || iso["returnPath"] != nil {
		t.Fatalf("isolation = %v err = %v", iso, err)
	}
	through := q
	through.Chain = []domain.TraceChainHop{{Transit: domain.TraceTransitThrough, Location: domain.TraceDevice("r2")}}
	if def, err := domain.ExistentialCheckDefinition(through, domain.IntentCheckOptions{}); err != nil || def["filters"].(map[string]any)["chain"] == nil {
		t.Fatalf("through = %v err = %v", def, err)
	}
	if _, err := domain.IsolationCheckDefinition(q, domain.IntentCheckOptions{ReturnPath: "ANY"}); err == nil {
		t.Fatal("a return path on an Isolation check must be refused")
	}
	if _, err := domain.ExistentialCheckDefinition(domain.TracePathQuery{}, domain.IntentCheckOptions{}); err == nil {
		t.Fatal("an empty path query must be refused")
	}

	nqe, err := domain.NQECheckDefinition(" Q_abc ", nil)
	if err != nil || nqe["checkType"] != "NQE" || nqe["queryId"] != "Q_abc" || nqe["params"] != nil {
		t.Fatalf("nqe = %v err = %v", nqe, err)
	}
	if err := domain.ValidateNewCheck(domain.NewCheck{Definition: nqe, Name: "x"}); err == nil {
		t.Fatal("a name on an NQE check must be flagged")
	}
	if err := domain.ValidateNewCheck(domain.NewCheck{Definition: nqe}); err != nil {
		t.Fatal(err)
	}
	if err := domain.ValidateNewCheck(domain.NewCheck{Definition: exists, Name: "tcp 8080", Tags: []string{"critical"}}); err != nil {
		t.Fatalf("an intent check takes a name and tags: %v", err)
	}
}

// Forward's refusal of a name, note or tags on an NQE check classifies, so a caller needs no prose matching; only the whole message does.
func TestCheckFieldNotAllowedKind(t *testing.T) {
	var message string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"` + message + `"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	nqe, _ := domain.NQECheckDefinition("Q_1", nil)
	for msg, want := range map[string]bool{
		"'name' not allowed for non-parameterized NQE checks":      true,
		"'note' not allowed for NQE checks":                        true,
		"'tags' not allowed for NQE checks":                        true,
		"'name' required for parameterized NQE checks":             true,
		"'tags' not allowed for Predefined checks":                 true,
		"'perfMonitoringEnabled' not allowed for Isolation checks": true,
		"'name' is required":                                       false,
	} {
		message = msg
		_, _, err := client.Checks.Create(context.Background(), "9", domain.NewCheck{Definition: nqe, Name: "x"}, nil)
		if got := errors.Is(err, domain.ErrCheckFieldNotAllowed); got != want {
			t.Errorf("%q: ErrCheckFieldNotAllowed = %v, want %v (%v)", msg, got, want, err)
		}
	}
}

// The field rules of NewNetworkCheck's constructor, written from its source: Predefined takes no name; an NQE check takes no name unless
// it has params, when it REQUIRES one; a note is refused only for NQE; tags only for the three intent types; perfMonitoringEnabled only
// for Existential.
func TestValidateNewCheckRules(t *testing.T) {
	plain, _ := domain.NQECheckDefinition("Q_1", nil)
	withParams, _ := domain.NQECheckDefinition("Q_1", map[string]any{"threshold": 3})
	predefined := map[string]any{"checkType": "Predefined", "predefinedCheckType": "VLAN_CONSISTENCY"}
	q := domain.TracePathQuery{From: &domain.TraceEndpoint{Location: domain.TraceDevice("a")}}
	exists, _ := domain.ExistentialCheckDefinition(q, domain.IntentCheckOptions{})
	isolation, _ := domain.IsolationCheckDefinition(q, domain.IntentCheckOptions{})
	on := true
	cases := []struct {
		name  string
		check domain.NewCheck
		ok    bool
	}{
		{"plain NQE, no fields", domain.NewCheck{Definition: plain}, true},
		{"plain NQE with a name", domain.NewCheck{Definition: plain, Name: "x"}, false},
		{"parameterized NQE without a name", domain.NewCheck{Definition: withParams}, false},
		{"parameterized NQE with a name", domain.NewCheck{Definition: withParams, Name: "threshold 3"}, true},
		{"parameterized NQE with a note", domain.NewCheck{Definition: withParams, Name: "n", Note: "x"}, false},
		{"NQE with tags", domain.NewCheck{Definition: plain, Tags: []string{"a"}}, false},
		{"predefined with a name", domain.NewCheck{Definition: predefined, Name: "x"}, false},
		{"predefined with a note", domain.NewCheck{Definition: predefined, Note: "x"}, true},
		{"predefined with tags", domain.NewCheck{Definition: predefined, Tags: []string{"a"}}, false},
		{"existential with name, note, tags", domain.NewCheck{Definition: exists, Name: "n", Note: "x", Tags: []string{"a"}}, true},
		{"existential with perfMonitoring", domain.NewCheck{Definition: exists, PerfMonitoringEnabled: &on}, true},
		{"isolation with perfMonitoring", domain.NewCheck{Definition: isolation, PerfMonitoringEnabled: &on}, false},
		{"isolation with tags", domain.NewCheck{Definition: isolation, Tags: []string{"a"}}, true},
		// The legacy spellings Forward still accepts are judged by the same rules.
		{"legacy NetworkQuery with a name", domain.NewCheck{Definition: map[string]any{"checkType": "NetworkQuery", "queryId": "Q_1"}, Name: "x"}, false},
		{"legacy NetworkQuery with a note", domain.NewCheck{Definition: map[string]any{"checkType": "NetworkQuery", "queryId": "Q_1"}, Note: "x"}, false},
		{"legacy IsolationCheck with perfMonitoring", domain.NewCheck{Definition: map[string]any{"checkType": "IsolationCheck"}, PerfMonitoringEnabled: &on}, false},
		{"legacy ExistentialCheck with perfMonitoring", domain.NewCheck{Definition: map[string]any{"checkType": "ExistentialCheck"}, PerfMonitoringEnabled: &on}, true},
		{"legacy Exists with tags", domain.NewCheck{Definition: map[string]any{"checkType": "Exists"}, Tags: []string{"a"}}, true},
	}
	for _, tc := range cases {
		if err := domain.ValidateNewCheck(tc.check); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok = %v", tc.name, err, tc.ok)
		}
	}
}
