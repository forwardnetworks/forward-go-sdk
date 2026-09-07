package forward

import (
	"context"
	"io"
	"net/http"
	"strings"
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

// The three JsonProp states must survive the round trip. Forward's patch DTO
// distinguishes ABSENT (leave the stored guests alone) from an empty list
// (delete every guest), and a Go slice cannot express both -- so the field is
// a pointer, and this test is here because getting it wrong deletes guests on
// a patch that meant to change nothing.
func TestControllerManagedSetupPatchDistinguishesAbsentFromEmpty(t *testing.T) {
	for name, tc := range map[string]struct {
		patch    ControllerManagedSetupPatch
		wantBody string
	}{
		"absent leaves the stored guests alone": {
			patch:    ControllerManagedSetupPatch{},
			wantBody: `{}`,
		},
		"an empty list clears every guest": {
			patch:    ControllerManagedSetupPatch{ManagedDevices: &[]ManagedDevice{}},
			wantBody: `{"managedDevices":[]}`,
		},
		"a populated list replaces them": {
			patch: ControllerManagedSetupPatch{ManagedDevices: &[]ManagedDevice{{
				Name: "cdg-vedge01", Type: "cisco_ios_xe_ssh",
				Host: "service-cdg-vedge01.example.svc.cluster.local", CLICredentialID: "L-77",
			}}},
			wantBody: `{"managedDevices":[{"name":"cdg-vedge01","type":"cisco_ios_xe_ssh",` +
				`"host":"service-cdg-vedge01.example.svc.cluster.local","cliCredentialId":"L-77"}]}`,
		},
	} {
		var gotBody, gotMethod, gotPath string
		c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
			gotMethod, gotPath = r.Method, r.URL.Path
			raw, _ := io.ReadAll(r.Body)
			gotBody = strings.TrimSpace(string(raw))
			_, _ = w.Write([]byte(`{"name":"sdwan","controllers":[{"name":"vsmart"}]}`))
		})
		out, _, err := c.ControllerManagedSetups.Patch(context.Background(), "net-1", "sdwan", tc.patch)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if gotMethod != http.MethodPatch {
			t.Errorf("%s: method %s, want PATCH", name, gotMethod)
		}
		if want := "/api/networks/net-1/controller-managed-setups/sdwan"; gotPath != want {
			t.Errorf("%s: path %q, want %q", name, gotPath, want)
		}
		if gotBody != tc.wantBody {
			t.Errorf("%s: body\n got %s\nwant %s", name, gotBody, tc.wantBody)
		}
		if out == nil || out.Name != "sdwan" {
			t.Errorf("%s: decoded setup %+v", name, out)
		}
	}
}

// A setup read back with guests must carry them, and one without must not
// invent an empty list -- "this setup has no guests declared" is the state the
// whole feature turns on, and it is how a re-sync knows there is work to do.
func TestControllerManagedSetupCarriesManagedDevices(t *testing.T) {
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"setups":[
			{"name":"sdwan","controllers":[{"name":"vsmart","type":"cisco_sdwan_vsmart_ssh"}],
			 "managedDevices":[{"name":"cdg-vedge01","type":"cisco_ios_xe_ssh","host":"h1","cliCredentialId":"L-77"}]},
			{"name":"bare","controllers":[{"name":"vsmart2"}]}]}`))
	})
	got, _, err := c.ControllerManagedSetups.List(context.Background(), "net-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d setups", len(got))
	}
	if len(got[0].ManagedDevices) != 1 {
		t.Fatalf("guests not carried: %+v", got[0])
	}
	guest := got[0].ManagedDevices[0]
	if guest.Name != "cdg-vedge01" || guest.Type != "cisco_ios_xe_ssh" || guest.CLICredentialID != "L-77" {
		t.Fatalf("guest decoded wrong: %+v", guest)
	}
	if got[1].ManagedDevices != nil {
		t.Fatalf("a setup with no managedDevices must decode as nil, got %+v", got[1].ManagedDevices)
	}
}

// The create body must carry guests when it has them and omit the key entirely
// when it does not: Forward treats an absent managedDevices as "controller-only
// setup", which is exactly what every setup Skyforge has created so far is.
func TestNewControllerManagedSetupOmitsGuestsWhenThereAreNone(t *testing.T) {
	for name, tc := range map[string]struct {
		in      NewControllerManagedSetup
		wantSub string
		absent  bool
	}{
		"no guests": {
			in:     NewControllerManagedSetup{Name: "sdwan", Controllers: []ControllerDevice{{Name: "vsmart", Host: "h"}}},
			absent: true,
		},
		"with guests": {
			in: NewControllerManagedSetup{
				Name:           "sdwan",
				Controllers:    []ControllerDevice{{Name: "vsmart", Host: "h"}},
				ManagedDevices: []ManagedDevice{{Name: "cdg-vedge01", Type: "cisco_ios_xe_ssh", Host: "h2", CLICredentialID: "L-77"}},
			},
			wantSub: `"managedDevices":[{"name":"cdg-vedge01","type":"cisco_ios_xe_ssh","host":"h2","cliCredentialId":"L-77"}]`,
		},
	} {
		var gotBody string
		c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			gotBody = string(raw)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"name":"sdwan","controllers":[{"name":"vsmart"}]}`))
		})
		if _, _, err := c.ControllerManagedSetups.Create(context.Background(), "net-1", tc.in); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if tc.absent {
			if strings.Contains(gotBody, "managedDevices") {
				t.Errorf("%s: body must omit managedDevices, got %s", name, gotBody)
			}
			continue
		}
		if !strings.Contains(gotBody, tc.wantSub) {
			t.Errorf("%s: body\n got %s\nwant substring %s", name, gotBody, tc.wantSub)
		}
	}
}
