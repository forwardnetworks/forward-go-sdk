package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocationsGetPatchDelete(t *testing.T) {
	t.Parallel()

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		switch {
		case r.URL.Path == "/api/networks/N1/locations/missing":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost:
			_, _ = io.WriteString(w, `{"id":"sjc","name":"San Jose","lat":37.3,"lng":-121.9,"city":"San Jose","country":"USA","deviceGlobs":["sjc-*"]}`)
		default:
			_, _ = io.WriteString(w, `{"id":"sjc","name":"San Jose DC","lat":37.3,"lng":-121.9,"city":"San Jose","country":"USA","deviceGlobs":["sjc-*","sjc2-*"]}`)
		}
	}))
	defer server.Close()
	locations := newTestClient(t, server.URL).Locations
	ctx := context.Background()

	created, _, err := locations.Create(ctx, "N1", LocationCreateRequest{ID: Ptr("sjc"), Name: "San Jose", Lat: 37.3, Lng: -121.9, City: "San Jose", Country: "USA", DeviceGlobs: []string{"sjc-*"}})
	if err != nil || created.ID != "sjc" || len(created.DeviceGlobs) != 1 || calls[0] != `POST /api/networks/N1/locations {"id":"sjc","name":"San Jose","lat":37.3,"lng":-121.9,"city":"San Jose","country":"USA","deviceGlobs":["sjc-*"]}` {
		t.Fatalf("Create() = %+v, %v; %q", created, err, calls[0])
	}
	got, _, err := locations.Get(ctx, "N1", "sjc")
	if err != nil || got.Name != "San Jose DC" || got.DeviceGlobs[1] != "sjc2-*" {
		t.Fatalf("Get() = %+v, %v", got, err)
	}
	if none, _, err := locations.Get(ctx, "N1", "missing"); none != nil || err != nil {
		t.Fatalf("absent Get() = %+v, %v", none, err)
	}
	patched, _, err := locations.Patch(ctx, "N1", "sjc", LocationPatch{Name: Ptr("San Jose DC"), DeviceGlobs: []string{"sjc-*", "sjc2-*"}})
	if err != nil || patched.Name != "San Jose DC" || calls[3] != `PATCH /api/networks/N1/locations/sjc {"deviceGlobs":["sjc-*","sjc2-*"],"name":"San Jose DC"}` {
		t.Fatalf("Patch() = %+v, %v; %q", patched, err, calls[3])
	}
	// An empty non-nil list clears the globs; nil leaves them.
	if _, _, err := locations.Patch(ctx, "N1", "sjc", LocationPatch{DeviceGlobs: []string{}}); err != nil || calls[4] != `PATCH /api/networks/N1/locations/sjc {"deviceGlobs":[]}` {
		t.Fatalf("clearing globs: %v %q", err, calls[4])
	}
	if _, err := locations.Delete(ctx, "N1", "sjc"); err != nil || calls[5] != "DELETE /api/networks/N1/locations/sjc " {
		t.Fatalf("Delete: %v %q", err, calls[5])
	}
	if _, err := locations.Delete(ctx, "N1", "missing"); err != nil {
		t.Fatalf("deleting a missing location must be success: %v", err)
	}
	before := len(calls)
	for name, f := range map[string]func() error{
		"empty patch": func() error { _, _, err := locations.Patch(ctx, "N1", "sjc", LocationPatch{}); return err },
		"blank rename": func() error {
			_, _, err := locations.Patch(ctx, "N1", "sjc", LocationPatch{Name: Ptr(" ")})
			return err
		},
		"blank id get": func() error { _, _, err := locations.Get(ctx, "N1", " "); return err },
		"blank delete": func() error { _, err := locations.Delete(ctx, "N1", ""); return err },
	} {
		if err := f(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if len(calls) != before {
		t.Fatalf("refused calls reached the wire: %q", calls[before:])
	}
}

// The atlas is a device-to-location map, not a geographic database; the grouped view says WHY a device is in
// a location (placed, anchored, or matched by deviceGlobs).
func TestLocationsAtlas(t *testing.T) {
	t.Parallel()

	var uris []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.URL.RequestURI())
		if r.URL.Query().Get("v") == "2" {
			_, _ = io.WriteString(w, `{"locations":[{"locationId":"den","devices":["den-r1"],"dynamicMatchDevices":["den-sw1","den-sw2"]},{"locationId":"sjc","anchoredDevices":["sjc-ctx1"]}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"den-r1":"den","den-sw1":"den","sjc-ctx1":"sjc"}`)
	}))
	defer server.Close()
	locations := newTestClient(t, server.URL).Locations
	ctx := context.Background()

	flat, _, err := locations.Atlas(ctx, "N1")
	if err != nil || len(flat) != 3 || flat["den-sw1"] != "den" || uris[0] != "/api/networks/N1/atlas" {
		t.Fatalf("Atlas() = %v, %v; %s", flat, err, uris[0])
	}
	grouped, _, err := locations.AtlasByLocation(ctx, "N1")
	if err != nil || uris[1] != "/api/networks/N1/atlas?v=2" || len(grouped) != 2 {
		t.Fatalf("AtlasByLocation() = %+v, %v; %s", grouped, err, uris[1])
	}
	if grouped[0].LocationID != "den" || len(grouped[0].DynamicMatchDevices) != 2 || len(grouped[0].AnchoredDevices) != 0 || grouped[1].AnchoredDevices[0] != "sjc-ctx1" {
		t.Fatalf("grouped = %+v", grouped)
	}
}
