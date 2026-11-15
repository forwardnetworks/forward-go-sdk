package forward

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
	q := TracePathQuery{
		From: &TraceEndpoint{Location: TraceSubnet("10.10.10.10/32"), Headers: []TraceHeaders{{"ip_proto": {"6"}, "tp_dst": {"8080"}}}},
		To:   &TraceEndpoint{Location: TraceSubnet("10.20.20.20/32")},
	}
	exists, err := ExistentialCheckDefinition(q, IntentCheckOptions{ReturnPath: "SYMMETRIC"})
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
	iso, err := IsolationCheckDefinition(q, IntentCheckOptions{})
	if err != nil || iso["checkType"] != "Isolation" || iso["returnPath"] != nil {
		t.Fatalf("isolation = %v err = %v", iso, err)
	}
	through := q
	through.Chain = []TraceChainHop{{Transit: TraceTransitThrough, Location: TraceDevice("r2")}}
	if def, err := ExistentialCheckDefinition(through, IntentCheckOptions{}); err != nil || def["filters"].(map[string]any)["chain"] == nil {
		t.Fatalf("through = %v err = %v", def, err)
	}
	if _, err := IsolationCheckDefinition(q, IntentCheckOptions{ReturnPath: "ANY"}); err == nil {
		t.Fatal("a return path on an Isolation check must be refused")
	}
	if _, err := ExistentialCheckDefinition(TracePathQuery{}, IntentCheckOptions{}); err == nil {
		t.Fatal("an empty path query must be refused")
	}

	nqe, err := NQECheckDefinition(" Q_abc ", nil)
	if err != nil || nqe["checkType"] != "NQE" || nqe["queryId"] != "Q_abc" || nqe["params"] != nil {
		t.Fatalf("nqe = %v err = %v", nqe, err)
	}
	if err := ValidateNewCheck(NewCheck{Definition: nqe, Name: "x"}); err == nil {
		t.Fatal("a name on an NQE check must be flagged")
	}
	if err := ValidateNewCheck(NewCheck{Definition: nqe}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateNewCheck(NewCheck{Definition: exists, Name: "tcp 8080", Tags: []string{"critical"}}); err != nil {
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
	nqe, _ := NQECheckDefinition("Q_1", nil)
	for msg, want := range map[string]bool{
		"'name' not allowed for non-parameterized NQE checks": true,
		"'note' not allowed for NQE checks":                   true,
		"'tags' not allowed for NQE checks":                   true,
		"'name' is required":                                  false,
	} {
		message = msg
		_, _, err := client.Checks.Create(context.Background(), "9", NewCheck{Definition: nqe, Name: "x"}, nil)
		if got := errors.Is(err, ErrCheckFieldNotAllowed); got != want {
			t.Errorf("%q: ErrCheckFieldNotAllowed = %v, want %v (%v)", msg, got, want, err)
		}
	}
}
