package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Names are unique across every collection source, so guests still held by
// classic devices make a plain Create fail with a 400. Forward's atomic way to
// take them over is POST ...controller-managed-setups?action=migrate, which
// deletes the overlapping classic devices and adds the setup in one
// transaction. It must be a distinct call, never a Create option a caller
// can reach by accident, and it must send the same body and the query.
func TestControllerManagedCreateMigratingClassicSendsActionMigrate(t *testing.T) {
	t.Parallel()

	var gotQuery, gotBody, gotMethod, migrateBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotQuery = r.Method, r.URL.RawQuery
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"sdwan"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	setup := NewControllerManagedSetup{Name: "sdwan", Controllers: []ControllerDevice{{Name: "vs1"}}}
	if _, _, err := client.ControllerManagedSetups.CreateMigratingClassic(context.Background(), "5", setup); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotQuery != "action=migrate" {
		t.Fatalf("got %s ?%s", gotMethod, gotQuery)
	}
	migrateBody = gotBody
	if _, _, err := client.ControllerManagedSetups.Create(context.Background(), "5", setup); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "" {
		t.Fatalf("a plain Create must never carry action=migrate, got ?%s", gotQuery)
	}
	if gotBody == "" || gotBody != migrateBody {
		t.Fatalf("both calls must send the same setup body: %q vs %q", migrateBody, gotBody)
	}
}

// The setup test is queued, not run: Forward answers 204 at once. The body must
// name devices or setups, never both or neither (Forward's rule, checked here so
// the caller gets the reason without a round trip), and a collector id rides in
// the query only when given.
func TestControllerManagedTestSelection(t *testing.T) {
	t.Parallel()

	var gotQuery, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	if _, err := client.ControllerManagedSetups.Test(ctx, "5", "", ControllerAndManagedDeviceSelection{SetupNames: []string{"sdwan"}}); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "action=test" || gotBody != `{"setupNames":["sdwan"]}` {
		t.Fatalf("got ?%s %s", gotQuery, gotBody)
	}
	if _, err := client.ControllerManagedSetups.Test(ctx, "5", "c1", ControllerAndManagedDeviceSelection{Names: []string{"vs1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ControllerManagedSetups.Test(ctx, "5", "", ControllerAndManagedDeviceSelection{SetupNames: []string{" SDWAN "}}); err != nil {
		t.Fatal(err)
	}
	if gotBody != `{"setupNames":["sdwan"]}` {
		t.Fatalf("setup names must be folded to the stored lowercase form, got %s", gotBody)
	}
	if _, err := client.ControllerManagedSetups.Test(ctx, "5", "c1", ControllerAndManagedDeviceSelection{Names: []string{"vs1"}}); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "action=test&collectorId=c1" {
		t.Fatalf("got ?%s", gotQuery)
	}
	gotQuery = "untouched"
	for _, bad := range []ControllerAndManagedDeviceSelection{{}, {Names: []string{"a"}, SetupNames: []string{"b"}}} {
		if _, err := client.ControllerManagedSetups.Test(ctx, "5", "", bad); err == nil {
			t.Fatalf("%+v must be refused", bad)
		}
	}
	if gotQuery != "untouched" {
		t.Fatal("a refused selection must not reach Forward")
	}
}
