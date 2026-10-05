package forward

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Each delete treats a plain 404 as "already gone", as Locations.Delete does, but a "No endpoint" 404 means Forward does
// not serve the route on this deployment and nothing was deleted: that must stay an error.
func TestV041DeletesTolerateGoneButNotAnUnservedRoute(t *testing.T) {
	t.Parallel()

	calls := map[string]func(*Client) (*Response, error){
		"DELETE /api/integrations/infoblox/instances/7": func(c *Client) (*Response, error) { return c.Integrations.DeleteInfoblox(context.Background(), "7") },
		"DELETE /api/networks/n1/rapid7-sources/scan%201": func(c *Client) (*Response, error) {
			return c.Integrations.DeleteRapid7(context.Background(), "n1", "scan 1")
		},
		"DELETE /api/networks/n1/jumpServers/j1": func(c *Client) (*Response, error) { return c.JumpServers.Delete(context.Background(), "n1", "j1") },
		"DELETE /api/custom-banners/b1":          func(c *Client) (*Response, error) { return c.Banners.Delete(context.Background(), "b1") },
		"DELETE /api/networks/n1/locations/l1/clusters/c%201": func(c *Client) (*Response, error) {
			return c.Locations.DeleteCluster(context.Background(), "n1", "l1", "c 1")
		},
	}
	for want, call := range calls {
		for _, unserved := range []bool{false, true} {
			var got string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Method + " " + r.URL.EscapedPath()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				if unserved {
					_, _ = io.WriteString(w, `{"apiUrl":"`+r.URL.Path+`","httpMethod":"DELETE","message":"No endpoint DELETE `+r.URL.Path+`."}`)
				} else {
					_, _ = io.WriteString(w, `{"message":"not found","reason":"NOT_FOUND"}`)
				}
			}))
			_, err := call(newTestClient(t, server.URL))
			server.Close()
			if got != want {
				t.Errorf("sent %q, want %q", got, want)
			}
			if unserved && !errors.Is(err, ErrEndpointNotServed) {
				t.Errorf("%s: an unserved route must surface ErrEndpointNotServed, got %v", want, err)
			}
			if !unserved && err != nil {
				t.Errorf("%s: an already-gone 404 must count as success, got %v", want, err)
			}
		}
	}
}

// Update bodies carry only the stated fields, and the secret fields reach the wire exactly when set.
func TestV041UpdatesSendOnlyStatedFields(t *testing.T) {
	t.Parallel()

	var method, path string
	var body map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.EscapedPath()
		data, _ := io.ReadAll(r.Body)
		body = nil
		_ = json.Unmarshal(data, &body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	ctx := context.Background()
	pw, port := "s3cret", 2222

	if _, err := c.JumpServers.Update(ctx, "n1", "j1", JumpServerUpdate{Password: &pw, Port: &port}); err != nil {
		t.Fatal(err)
	}
	if method != "PATCH" || path != "/api/networks/n1/jumpServers/j1" || len(body) != 2 || string(body["password"]) != `"s3cret"` || string(body["port"]) != "2222" {
		t.Errorf("jump server update = %s %s %v", method, path, body)
	}
	name := "dns"
	if _, err := c.Integrations.UpdateInfoblox(ctx, "7", InfobloxInstanceUpdate{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if method != "PATCH" || path != "/api/integrations/infoblox/instances/7" || len(body) != 1 || string(body["name"]) != `"dns"` {
		t.Errorf("infoblox update = %s %s %v", method, path, body)
	}
	if _, err := c.Snapshots.Unfavorite(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if method != "PATCH" || path != "/api/snapshots/s1" {
		t.Errorf("unfavorite = %s %s", method, path)
	}
	for _, bad := range []string{"", "abc", "-1"} {
		if _, err := c.Integrations.UpdateInfoblox(ctx, bad, InfobloxInstanceUpdate{}); err == nil {
			t.Errorf("Infoblox instance id %q must be refused before any request", bad)
		}
	}
}
