package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Each call must hit Forward's exact route and query: the managed-decorator toggle
// is a PATCH with ?enabled=, and Decorate needs ?type= (Forward refuses the call
// without it) plus snapshotId only when asked for one.
func TestDecoratorsWirePaths(t *testing.T) {
	var seen []string
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		switch {
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[{"name":"fwd-Path MTU","type":"PATH","queryId":"FQ_a","metadataColNames":[],"enabled":false,"systemManaged":true},{"name":"Owner","type":"DEVICE","queryId":"Q_1","metadataColNames":["owner"],"deviceColName":"device","enabled":true,"systemManaged":false,"createdBy":"craig"}]`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/decorators/"):
			_, _ = w.Write([]byte(`{"name":"my dec","type":"DEVICE","queryId":"Q_1","metadataColNames":[],"deviceColName":"device","enabled":false,"systemManaged":false}`))
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"entityDecorations":[{"entityId":"r1","decoratorName":"Owner","status":"SUCCESS","rowType":{"type":"RECORD"},"rows":[{"owner":"neteng"}]},{"entityId":"r2","decoratorName":"Owner","status":"INVALID_DECORATOR"}]}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	ctx := context.Background()
	all, _, err := c.Decorators.List(ctx)
	if err != nil || len(all) != 2 || !all[0].SystemManaged || all[1].DeviceColName != "device" || all[1].CreatedBy != "craig" {
		t.Fatalf("list: %v %+v", err, all)
	}
	if got, _, err := c.Decorators.Get(ctx, "owner"); err != nil || got == nil || got.Name != "Owner" {
		t.Fatalf("get is case-insensitive: %v %+v", err, got)
	}
	if got, _, err := c.Decorators.Get(ctx, "nope"); err != nil || got != nil {
		t.Fatalf("a missing decorator is nil without an error: %v %+v", err, got)
	}
	if _, _, err := c.Decorators.Put(ctx, "my dec", DecoratorPut{Type: DecoratorEntityDevice, QueryID: "Q_1", DeviceColName: "device", MetadataColNames: &[]string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Decorators.Delete(ctx, "my dec"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Decorators.SetManagedEnabled(ctx, "fwd-Path MTU", true); err != nil {
		t.Fatal(err)
	}
	dec, _, err := c.Decorators.Decorate(ctx, "n1", "", DecoratorEntityDevice, DecorateRequest{EntityIDs: []string{"r1", "r2"}, DecoratorNames: []string{"Owner"}})
	if err != nil || len(dec) != 2 || dec[0].Status != DecorationSuccess || len(dec[0].Rows) != 1 || dec[1].Status != DecorationInvalidDecorator || len(dec[1].Rows) != 0 {
		t.Fatalf("decorate: %v %+v", err, dec)
	}
	if _, _, err := c.Decorators.Decorate(ctx, "n1", "s9", DecoratorEntityPath, DecorateRequest{EntityIDs: []string{"p"}, DecoratorNames: []string{"d"}}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /api/decorators?", "GET /api/decorators?", "GET /api/decorators?",
		"PATCH /api/decorators/my%20dec?",
		"DELETE /api/decorators/my%20dec?",
		"PATCH /api/system-managed-decorators/fwd-Path%20MTU?enabled=true",
		"POST /api/networks/n1/decorations?type=DEVICE",
		"POST /api/networks/n1/decorations?snapshotId=s9&type=PATH",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}

// Forward merges a PATCH onto the existing decorator, so an unset field must be
// absent from the body (a sent "" or false would overwrite), while a non-nil empty
// column list must be sent as [] because that is how "all columns" is spelled.
func TestDecoratorPutBodyOmitsUnsetFields(t *testing.T) {
	var bodies []map[string]any
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		bodies = append(bodies, m)
		_, _ = w.Write([]byte(`{"name":"d","type":"HOST","queryId":"Q","metadataColNames":[],"enabled":true,"systemManaged":false}`))
	})
	ctx := context.Background()
	all := []string{}
	if _, _, err := c.Decorators.Put(ctx, "d", DecoratorPut{Type: DecoratorEntityHost}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Decorators.Put(ctx, "d", DecoratorPut{Type: DecoratorEntityHost, IPAddressColName: "ip", MACAddressColName: "mac", MetadataColNames: &all, Enabled: Ptr(true)}); err != nil {
		t.Fatal(err)
	}
	// A new decorator takes its name from the body, so the path name is sent as the body name; nothing else is.
	if len(bodies[0]) != 2 || bodies[0]["type"] != "HOST" || bodies[0]["name"] != "d" {
		t.Fatalf("an empty put must send only type and the path name: %v", bodies[0])
	}
	b := bodies[1]
	if cols, ok := b["metadataColNames"].([]any); !ok || len(cols) != 0 || b["enabled"] != true || b["ipAddressColName"] != "ip" || b["macAddressColName"] != "mac" {
		t.Fatalf("body %v", b)
	}
}

// A delete of a decorator that is already gone is success (Forward answers 404),
// but a route this deployment does not serve is not; and bad input must be refused
// before any request, which a server that answers 403 to everything would hide.
func TestDecoratorsDeleteAndValidation(t *testing.T) {
	requests := 0
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/api/decorators/gone":
			w.WriteHeader(http.StatusNotFound)
		case "/api/decorators/unserved":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No endpoint DELETE /api/decorators/unserved."}`))
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	})
	ctx := context.Background()
	if _, err := c.Decorators.Delete(ctx, "gone"); err != nil {
		t.Fatalf("404 must be success: %v", err)
	}
	if _, err := c.Decorators.Delete(ctx, "unserved"); err == nil {
		t.Fatal("a route-not-served 404 must stay an error")
	}
	if _, err := c.Decorators.Delete(ctx, "kept"); err == nil {
		t.Fatal("403 must stay an error")
	}
	requests = 0
	for name, err := range map[string]error{
		"put no name": func() error {
			_, _, e := c.Decorators.Put(ctx, " ", DecoratorPut{Type: DecoratorEntityDevice})
			return e
		}(),
		"put bad type": func() error { _, _, e := c.Decorators.Put(ctx, "d", DecoratorPut{Type: "ROUTER"}); return e }(),
		"decorate no entities": func() error {
			_, _, e := c.Decorators.Decorate(ctx, "n1", "", DecoratorEntityDevice, DecorateRequest{DecoratorNames: []string{"d"}})
			return e
		}(),
		"decorate no type": func() error {
			_, _, e := c.Decorators.Decorate(ctx, "n1", "", "", DecorateRequest{EntityIDs: []string{"a"}, DecoratorNames: []string{"d"}})
			return e
		}(),
		"managed no name": func() error { _, e := c.Decorators.SetManagedEnabled(ctx, "", true); return e }(),
	} {
		if err == nil {
			t.Errorf("%s: want a client-side refusal", name)
		}
	}
	if requests != 0 {
		t.Fatalf("%d invalid calls reached the server", requests)
	}
}
