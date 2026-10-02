package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The schema is handed back byte for byte: callers parse InlinedType with
// their own code, and the snapshot variant only adds ?snapshotId=.
func TestNQESchema(t *testing.T) {
	t.Parallel()

	const tree = `{"type":"record","fields":[{"name":"network","isNullable":false,"type":{"type":"record","fields":[]}}]}`
	var uris []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.URL.RequestURI())
		_, _ = io.WriteString(w, tree)
	}))
	defer server.Close()
	nqe := newTestClient(t, server.URL).NQE
	ctx := context.Background()

	raw, _, err := nqe.Schema(ctx)
	if err != nil || string(raw) != tree || uris[0] != "/api/nqe/schema" {
		t.Fatalf("Schema() = %s, %v; %v", raw, err, uris)
	}
	raw, _, err = nqe.SchemaAt(ctx, " 1021 ")
	if err != nil || string(raw) != tree || uris[1] != "/api/nqe/schema?snapshotId=1021" {
		t.Fatalf("SchemaAt() = %s, %v; %v", raw, err, uris)
	}
	if _, _, err := nqe.SchemaAt(ctx, ""); err == nil {
		t.Fatal("an empty snapshot ID must be refused, not fall back to the org schema")
	}
}
