package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionGet(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/version" {
			t.Errorf("request = %s %s", r.Method, r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"build":"abc","release":"26.30.0-01","version":"26.30.0"}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	got, resp, err := client.Version.Get(context.Background())
	if err != nil {
		t.Fatalf("Version.Get() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK || got.Build != "abc" || got.Version != "26.30.0" {
		t.Fatalf("Version.Get() = %#v, status %d", got, resp.StatusCode)
	}
}

func TestNetworksCRUD(t *testing.T) {
	t.Parallel()

	var patch NetworkUpdate
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "user" || pass != "pass" {
			t.Errorf("BasicAuth() = %q, %q, %v", user, pass, ok)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/networks":
			_, _ = io.WriteString(w, `[{"id":"network-1","name":"Lab","orgId":"org-1"}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks":
			if got := r.URL.Query().Get("name"); got != "Lab & QA" {
				t.Errorf("create name = %q", got)
			}
			_, _ = io.WriteString(w, `{"id":"network-2","name":"Lab & QA","orgId":"org-1"}`)
		case r.Method == http.MethodPatch && r.URL.EscapedPath() == "/api/networks/network%2F2":
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Errorf("decode update: %v", err)
			}
			_, _ = io.WriteString(w, `{"id":"network/2","name":"Renamed","orgId":"org-1"}`)
		case r.Method == http.MethodDelete && r.URL.EscapedPath() == "/api/networks/network%2F2":
			_, _ = io.WriteString(w, `{"id":"network/2","name":"Renamed","orgId":"org-1"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()

	networks, _, err := client.Networks.List(ctx)
	if err != nil || len(networks) != 1 || networks[0].ID != "network-1" {
		t.Fatalf("Networks.List() = %#v, %v", networks, err)
	}
	created, _, err := client.Networks.Create(ctx, " Lab & QA ")
	if err != nil || created.ID != "network-2" {
		t.Fatalf("Networks.Create() = %#v, %v", created, err)
	}

	name := "Renamed"
	updated, _, err := client.Networks.Update(ctx, "network/2", NetworkUpdate{Name: &name})
	if err != nil || updated.Name != name {
		t.Fatalf("Networks.Update() = %#v, %v", updated, err)
	}
	if patch.Name == nil || *patch.Name != name {
		t.Fatalf("update body = %#v", patch)
	}

	deleted, _, err := client.Networks.Delete(ctx, "network/2")
	if err != nil || deleted.ID != "network/2" {
		t.Fatalf("Networks.Delete() = %#v, %v", deleted, err)
	}
}

func TestNetworkInputValidation(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, "https://fwd.example")
	if _, _, err := client.Networks.Create(context.Background(), "  "); err == nil {
		t.Fatal("Networks.Create() error = nil")
	}
	if _, _, err := client.Networks.Delete(context.Background(), "  "); err == nil {
		t.Fatal("Networks.Delete() error = nil")
	}
}
