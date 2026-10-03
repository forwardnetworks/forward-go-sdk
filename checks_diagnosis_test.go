package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A loop violation (predefined NO_LOOP) sends diagnosis.details[].query as an
// object, not the string Forward's spec promises; that must not fail the read.
func TestChecksGetToleratesObjectQuery(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"241352","status":"FAIL","numViolations":2,"diagnosis":{"summary":"","detailsIncomplete":false,"details":[
		  {"query":{"flowTypes":["LOOP"]}},
		  {"query":"from d to e","references":[{"key":"k","value":"v"}]},
		  {"query":null},
		  {"references":[{"key":"only"}]}]}}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).Checks.Get(context.Background(), "13958", "241352")
	if err != nil {
		t.Fatalf("Get() must not fail on an object-form query: %v", err)
	}
	d := got.Diagnosis.Details
	if len(d) != 4 || d[0].Query != "" || string(d[0].QueryRaw) != `{"flowTypes":["LOOP"]}` || len(d[0].FlowTypes()) != 1 || d[0].FlowTypes()[0] != "LOOP" {
		t.Fatalf("object form = %+v", d[0])
	}
	if d[1].Query != "from d to e" || d[1].FlowTypes() != nil || d[1].References[0].Key != "k" {
		t.Fatalf("string form = %+v", d[1])
	}
	if d[2].QueryRaw != nil || d[3].References[0].Key != "only" {
		t.Fatalf("null and absent query = %+v %+v", d[2], d[3])
	}
}
