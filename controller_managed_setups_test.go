package forward

import (
	"context"
	"net/http"
	"testing"
)

// A list this client cannot read is NOT an empty list. Forward wraps setups
// as {"setups": [...]}, but an under-read here does not error -- it returns
// nothing, which reads as "no setup exists" and sends the caller into a
// create that collides with the setup sitting right there:
// "Controller-managed setup named 'sdwan' already exists in network",
// measured on cs-lab network 3150.
func TestControllerManagedSetupsListAcceptsForwardsEnvelopes(t *testing.T) {
	for name, body := range map[string]string{
		"setups wrapper": `{"setups":[{"name":"sdwan","controllers":[{"name":"vmanage"}]}]}`,
		"bare array":     `[{"name":"sdwan","controllers":[{"name":"vmanage"}]}]`,
		"data wrapper":   `{"data":[{"name":"sdwan","controllers":[{"name":"vmanage"}]}]}`,
	} {
		c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		})
		got, _, err := c.ControllerManagedSetups.List(context.Background(), "net-1")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 1 || got[0].Name != "sdwan" || len(got[0].Controllers) != 1 {
			t.Fatalf("%s: got %+v", name, got)
		}
	}
}

// An unreadable body must be an ERROR, never an empty list. This is the
// property the whole reconciliation rests on: an empty list means "no setup
// exists" and sends the caller into a create, so a misread body is how a
// drifted setup survives forever.
func TestControllerManagedSetupsListRefusesBodiesItCannotRead(t *testing.T) {
	for name, body := range map[string]string{
		"an HTML login page":       `<html>login</html>`,
		"an error object":          `{"error":"forbidden"}`,
		"an object with no setups": `{"unexpected":[]}`,
	} {
		c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		})
		if _, _, err := c.ControllerManagedSetups.List(context.Background(), "net-1"); err == nil {
			t.Errorf("%s was accepted as an empty setup list", name)
		}
	}
	// Genuinely empty stays empty, and is not an error.
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"setups":[]}`))
	})
	got, _, err := c.ControllerManagedSetups.List(context.Background(), "net-1")
	if err != nil || len(got) != 0 {
		t.Fatalf("an empty list must be empty and not an error: %v %v", got, err)
	}
}

// Delete is idempotent, and neither call may escape to the collection.
func TestControllerManagedSetupDeleteIsIdempotentAndScoped(t *testing.T) {
	var path string
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := c.ControllerManagedSetups.Delete(context.Background(), "net-1", "sdwan"); err != nil {
		t.Fatalf("404 must be success: %v", err)
	}
	if path != "/api/networks/net-1/controller-managed-setups/sdwan" {
		t.Fatalf("path was %q", path)
	}
	if _, err := c.ControllerManagedSetups.Delete(context.Background(), "net-1", "  "); err == nil {
		t.Fatal("an empty setup name must be refused, not deleted as a collection")
	}
}

// A setup with no controllers is meaningless and Forward requires them, so it
// is refused where the message can say so.
func TestControllerManagedSetupCreateRequiresNameAndControllers(t *testing.T) {
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"sdwan"}`))
	})
	ctx := context.Background()
	if _, _, err := c.ControllerManagedSetups.Create(ctx, "net-1", NewControllerManagedSetup{Controllers: []ControllerDevice{{Name: "v"}}}); err == nil {
		t.Fatal("an empty name must be refused")
	}
	if _, _, err := c.ControllerManagedSetups.Create(ctx, "net-1", NewControllerManagedSetup{Name: "sdwan"}); err == nil {
		t.Fatal("a setup with no controllers must be refused")
	}
	if _, _, err := c.ControllerManagedSetups.Create(ctx, "net-1", NewControllerManagedSetup{
		Name: "sdwan", Controllers: []ControllerDevice{{Name: "vmanage", Type: "cisco_vmanage", Host: "10.0.0.1"}},
	}); err != nil {
		t.Fatalf("valid create: %v", err)
	}
}
