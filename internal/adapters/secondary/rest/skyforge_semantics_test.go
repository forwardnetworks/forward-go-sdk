package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForNetworkSharesTransportAndScopesServices(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/networks/tenant-2/snapshots" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":12,"state":"PROCESSED"}]`)
	}))
	defer server.Close()

	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "user", Password: "pass", NetworkID: "tenant-1"})
	if err != nil {
		t.Fatal(err)
	}
	scoped := client.ForNetwork("tenant-2")
	if scoped == client || scoped.httpClient != client.httpClient || scoped.capabilities != client.capabilities {
		t.Fatal("ForNetwork must clone scope while sharing transport and capability state")
	}
	if client.Network() != "tenant-1" || scoped.Network() != "tenant-2" {
		t.Fatalf("network scopes = %q, %q", client.Network(), scoped.Network())
	}
	snapshots, _, err := scoped.Snapshots.List(context.Background(), "", ports.SnapshotListOptions{})
	if err != nil || len(snapshots) != 1 || snapshots[0].ID != "12" {
		t.Fatalf("scoped Snapshots.List() = %#v, %v", snapshots, err)
	}
}
