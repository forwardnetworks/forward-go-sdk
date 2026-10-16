package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CloudAccounts took an explicit network ID while every other network-scoped
// service resolves an empty one from the client's bound network. A client made
// with ForNetwork must reach its own network's accounts.
func TestCloudAccountsUsesTheBoundNetwork(t *testing.T) {
	t.Parallel()

	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.Contains(r.URL.Path, "aws-cap-credentials") {
			_, _ = w.Write([]byte(`{"id":"cap-1"}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	if _, _, err := client.CloudAccounts.List(context.Background(), ""); err == nil {
		t.Fatal("an unbound client with no network ID must be refused")
	}
	if _, _, err := client.ForNetwork("net-7").CloudAccounts.List(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.ForNetwork("net-7").CloudAccounts.GetAWSCapCredential(context.Background(), "", "cap-1"); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/api/networks/net-7/cloudAccounts" || paths[1] != "/api/networks/net-7/aws-cap-credentials/cap-1" {
		t.Fatalf("paths = %v", paths)
	}
}
