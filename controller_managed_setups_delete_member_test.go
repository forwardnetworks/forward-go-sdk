package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Removing one member must hit the member's own route, not the whole-list PATCH: the caller's point is not restating the other
// members. The setup name is lowercased (Forward stores it so) and the member name is sent as given.
func TestDeleteControllerManagedMemberRoutes(t *testing.T) {
	var seen []string
	status := http.StatusNoContent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.EscapedPath())
		w.WriteHeader(status)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	ctx := context.Background()
	if _, err := c.ControllerManagedSetups.DeleteController(ctx, "N1", "Vsm", "c 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ControllerManagedSetups.DeleteManagedDevice(ctx, "N1", "Vsm", "edge1"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"DELETE /api/networks/N1/controller-managed-setups/vsm/controllers/c%201",
		"DELETE /api/networks/N1/controller-managed-setups/vsm/managed-devices/edge1",
	}
	if len(seen) != 2 || seen[0] != want[0] || seen[1] != want[1] {
		t.Fatalf("requests = %v, want %v", seen, want)
	}

	// A 404 means the member is already gone: success. A 400 (the last member) must reach the caller.
	status = http.StatusNotFound
	if _, err := c.ControllerManagedSetups.DeleteManagedDevice(ctx, "N1", "vsm", "gone"); err != nil {
		t.Errorf("a 404 must be success, got %v", err)
	}
	status = http.StatusBadRequest
	if _, err := c.ControllerManagedSetups.DeleteController(ctx, "N1", "vsm", "last"); err == nil {
		t.Error("a 400 must be returned")
	}

	n := len(seen)
	if _, err := c.ControllerManagedSetups.DeleteController(ctx, "N1", "vsm", " "); err == nil {
		t.Error("an empty member name must be refused")
	}
	if _, err := c.ControllerManagedSetups.DeleteManagedDevice(ctx, "N1", "", "x"); err == nil {
		t.Error("an empty setup name must be refused")
	}
	if len(seen) != n {
		t.Error("a refused call must send nothing")
	}
}
