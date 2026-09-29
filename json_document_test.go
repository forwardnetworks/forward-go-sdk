package forward

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A *Document method exists to hand the caller Forward's JSON untouched. That
// only works if JSONDocument is json.RawMessage itself: a type merely defined
// from it drops RawMessage's methods, so encoding/json treats it as []byte,
// wants a base64 string, and fails on every real object Forward sends.
func TestJSONDocumentMethodsReturnTheBodyVerbatim(t *testing.T) {
	t.Parallel()

	const body = `{"metrics":[{"deviceName":"leaf-1","value":42.5}],"nested":{"a":[1,2]}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	query := MetricQuery{}

	calls := map[string]func() (JSONDocument, *Response, error){
		"Performance.DeviceMetricsDocument": func() (JSONDocument, *Response, error) {
			return client.Performance.DeviceMetricsDocument(ctx, "net-1", query)
		},
		"Performance.DeviceMetricHistoryDocument": func() (JSONDocument, *Response, error) {
			return client.Performance.DeviceMetricHistoryDocument(ctx, "net-1", query, DeviceMetricHistoryRequest{})
		},
		"Performance.InterfaceMetricsDocument": func() (JSONDocument, *Response, error) {
			return client.Performance.InterfaceMetricsDocument(ctx, "net-1", query)
		},
		"Performance.InterfaceMetricHistoryDocument": func() (JSONDocument, *Response, error) {
			return client.Performance.InterfaceMetricHistoryDocument(ctx, "net-1", query, InterfaceMetricHistoryRequest{})
		},
		"Performance.UnhealthyDevices": func() (JSONDocument, *Response, error) {
			return client.Performance.UnhealthyDevices(ctx, "net-1", query)
		},
		"Performance.UnhealthyInterfaces": func() (JSONDocument, *Response, error) {
			return client.Performance.UnhealthyInterfaces(ctx, "net-1", query, UnhealthyInterfacesRequest{Devices: []string{"leaf-1"}})
		},
		"Snapshots.ListDocument": func() (JSONDocument, *Response, error) {
			return client.Snapshots.ListDocument(ctx, "net-1", SnapshotListOptions{})
		},
	}
	for name, call := range calls {
		doc, _, err := call()
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if string(doc) != body {
			t.Errorf("%s = %s, want the body verbatim", name, doc)
		}
	}
}

// Re-encoding a document inside a caller's own response must embed the JSON,
// not a base64 string of its bytes.
func TestJSONDocumentMarshalsAsJSON(t *testing.T) {
	t.Parallel()

	wrapped, err := json.Marshal(struct {
		Doc JSONDocument `json:"doc"`
	}{Doc: JSONDocument(`{"a":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if string(wrapped) != `{"doc":{"a":1}}` {
		t.Fatalf("marshaled = %s", wrapped)
	}
}
