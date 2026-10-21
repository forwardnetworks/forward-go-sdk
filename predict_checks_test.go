package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Change-set checks share Forward's CheckDefinition (checkType discriminator) with network checks but accept only four types, and the
// wire body is {name?, definition}. The SDK refuses the combinations Forward's ChangeSetCheck/IntentCheck/NqeCheck constructors refuse
// so the mistake surfaces before a request.
func TestChangeSetCheckDefinitionValidation(t *testing.T) {
	t.Parallel()

	filters := json.RawMessage(`{"from":{"location":{"type":"SubnetLocationFilter","value":"10.0.0.0/8"}}}`)
	good := []ChangeSetCheckDefinition{
		{CheckType: ChangeSetCheckIsolation, Filters: filters, NoiseTypes: []string{"GLEAN"}},
		{CheckType: ChangeSetCheckReachability, Filters: filters, HeaderFieldsWithDefaults: []string{"ip_tos"}},
		{CheckType: ChangeSetCheckExists, Filters: filters, ReturnPath: ReturnPathSymmetric},
		{CheckType: ChangeSetCheckNQE, QueryID: "Q_abc", Params: map[string]json.RawMessage{"n": json.RawMessage(`3`)}},
	}
	for _, d := range good {
		if err := d.Validate(); err != nil {
			t.Errorf("%s: %v", d.CheckType, err)
		}
	}
	bad := map[string]ChangeSetCheckDefinition{
		"unsupported type":            {CheckType: "Predefined"},
		"intent with no filters":      {CheckType: ChangeSetCheckIsolation},
		"intent with null filters":    {CheckType: ChangeSetCheckIsolation, Filters: json.RawMessage(`null`)},
		"returnPath on isolation":     {CheckType: ChangeSetCheckIsolation, Filters: filters, ReturnPath: ReturnPathAny},
		"unknown returnPath":          {CheckType: ChangeSetCheckExists, Filters: filters, ReturnPath: "BOTH"},
		"query on an intent check":    {CheckType: ChangeSetCheckReachability, Filters: filters, QueryID: "Q"},
		"nqe with no query":           {CheckType: ChangeSetCheckNQE},
		"nqe with filters":            {CheckType: ChangeSetCheckNQE, QueryID: "Q", Filters: filters},
		"nqe with intent noise types": {CheckType: ChangeSetCheckNQE, QueryID: "Q", NoiseTypes: []string{}},
	}
	for name, d := range bad {
		if d.Validate() == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestChangeSetCheckAddUpdateDeleteWire(t *testing.T) {
	t.Parallel()

	type seen struct{ method, path, body string }
	var requests []seen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, seen{r.Method, r.URL.RequestURI(), string(body)})
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"7","name":"c","definition":{"checkType":"Existential","filters":{"to":{}},"returnPath":"ANY"},"status":"PASS"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	filters := json.RawMessage(`{"to":{}}`)

	added, _, err := client.Predict.AddChangeSetCheck(ctx, "N1", "cs 1", NewChangeSetCheck{Name: "c", Definition: ChangeSetCheckDefinition{CheckType: ChangeSetCheckExists, Filters: filters, ReturnPath: ReturnPathAny}})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := added.ParsedDefinition()
	if err != nil || parsed.CheckType != ChangeSetCheckExists || parsed.ReturnPath != "ANY" || added.Status != "PASS" {
		t.Fatalf("result: %+v %+v %v", added, parsed, err)
	}
	if _, err := client.Predict.UpdateChangeSetCheck(ctx, "N1", "cs 1", "7", ChangeSetCheckPatch{ClearName: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Predict.UpdateChangeSetCheck(ctx, "N1", "cs 1", "7", ChangeSetCheckPatch{Name: Ptr("renamed"), Definition: &ChangeSetCheckDefinition{CheckType: ChangeSetCheckIsolation, Filters: filters}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Predict.DeleteChangeSetCheck(ctx, "N1", "cs 1", "7"); err != nil {
		t.Fatal(err)
	}
	want := []seen{
		{"POST", "/api/networks/N1/change-sets/cs%201/checks", `{"name":"c","definition":{"checkType":"Existential","filters":{"to":{}},"returnPath":"ANY"}}`},
		// An explicit JSON null is how Forward's JsonProp tells "clear the name" from "leave it alone".
		{"PATCH", "/api/networks/N1/change-sets/cs%201/checks/7", `{"name":null}`},
		{"PATCH", "/api/networks/N1/change-sets/cs%201/checks/7", `{"definition":{"checkType":"Isolation","filters":{"to":{}}},"name":"renamed"}`},
		{"DELETE", "/api/networks/N1/change-sets/cs%201/checks/7", ``},
	}
	if len(requests) != len(want) {
		t.Fatalf("%d requests, want %d: %+v", len(requests), len(want), requests)
	}
	for i, w := range want {
		if requests[i] != w {
			t.Errorf("request %d = %+v, want %+v", i, requests[i], w)
		}
	}

	// Refusals happen before any request.
	before := len(requests)
	if _, _, err := client.Predict.AddChangeSetCheck(ctx, "N1", "cs", NewChangeSetCheck{Name: "q", Definition: ChangeSetCheckDefinition{CheckType: ChangeSetCheckNQE, QueryID: "Q"}}); err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("a named NQE check must be refused: %v", err)
	}
	if _, err := client.Predict.UpdateChangeSetCheck(ctx, "N1", "cs", "7", ChangeSetCheckPatch{}); err == nil {
		t.Error("an empty patch must be refused")
	}
	if _, err := client.Predict.UpdateChangeSetCheck(ctx, "N1", "cs", "7", ChangeSetCheckPatch{Name: Ptr("x"), ClearName: true}); err == nil {
		t.Error("Name and ClearName together must be refused")
	}
	if _, err := client.Predict.DeleteChangeSetCheck(ctx, "N1", "cs", " "); err == nil {
		t.Error("a check id is required")
	}
	if len(requests) != before {
		t.Errorf("a refused call sent %d request(s)", len(requests)-before)
	}
}

// Forward treats an absent headerFieldsWithDefaults as "apply the global default {url}" and [] as "no header defaults", and compares
// definitions for equality (a changed definition marks every result outdated). Reading a check, changing its filters and writing it
// back must therefore keep an empty list empty and an absent list absent.
func TestChangeSetCheckDefinitionKeepsEmptyListsDistinctFromAbsentOnes(t *testing.T) {
	t.Parallel()

	for body, want := range map[string]string{
		`{"checkType":"Isolation","filters":{"to":{}},"noiseTypes":[],"headerFieldsWithDefaults":[]}`: `{"checkType":"Isolation","filters":{"to":{}},"headerFieldsWithDefaults":[],"noiseTypes":[]}`,
		`{"checkType":"Isolation","filters":{"to":{}}}`:                                               `{"checkType":"Isolation","filters":{"to":{}}}`,
		`{"checkType":"NQE","queryId":"Q_1","params":{"big":9007199254740993}}`:                       `{"checkType":"NQE","params":{"big":9007199254740993},"queryId":"Q_1"}`,
	} {
		var def ChangeSetCheckDefinition
		if err := json.Unmarshal([]byte(body), &def); err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(def)
		if err != nil || string(got) != want {
			t.Errorf("round trip of %s = %s (%v), want %s", body, got, err, want)
		}
	}
}
