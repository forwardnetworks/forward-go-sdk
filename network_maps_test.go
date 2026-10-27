package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mapsServer answers the NetworkMapController read routes with bodies shaped like the appserver's (NetworkMapInfo, NetworkMap,
// NetworkMapSnapshot, StructureNode, flattened NetworkMapDirectories) and records every request URI.
func mapsServer(t *testing.T, uris *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*uris = append(*uris, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/networks/7/maps":
			_, _ = io.WriteString(w, `{"maps":[{"id":"3","name":"Core","pinned":true,"createdById":"12","createdAt":"2026-10-01T10:00:00Z","createdBy":"mary","updatedById":"13","updatedAt":"2026-10-02T10:00:00Z","updatedBy":"joe"},{"id":"4","name":"Edge"}]}`)
		case "/api/networks/8/maps":
			_, _ = io.WriteString(w, `{"maps":[]}`)
		case "/api/networks/9/maps":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"Missing permission: NetworkOperation.VIEW_NETWORK_MAPS"}`)
		case "/api/networks/7/maps/3":
			if r.URL.Query().Get("view") == "snapshot" {
				_, _ = io.WriteString(w, `{"id":"3","name":"Core","devices":[{"name":"Core-1","type":"ROUTER","vendor":"CISCO","platform":"IOS_XE","managementIps":["10.0.0.1"],"collectionIp":"10.0.0.1","tags":["core","dc1"],"locationId":"L7","x":1.5,"y":2},{"name":"core-2"}],"interfaces":[{"device":"Core-1","interfaces":[]}],"links":[],"bgpLinks":[]}`)
				return
			}
			directory := ""
			if r.URL.Query().Get("with") == "directory" {
				directory = `,"directory":"/prod"`
			}
			_, _ = io.WriteString(w, `{"id":"3","name":"Core","devices":[{"name":"core-1","x":1.5,"y":2},{"name":"core-2"},{"name":"gone-9"}],"deviceDefaults":{"label":"name"}`+directory+`}`)
		case "/api/networks/7/maps/3/structures":
			_, _ = io.WriteString(w, `[{"type":"SPINE_LEAF","devices":["Core-1"],"structures":[{"type":"DEVICE_ROW","devices":["Core-2"]}]}]`)
		case "/api/networks/8/map-directories":
			_, _ = io.WriteString(w, `{"directories":[{"path":"/","mapIds":[]}]}`)
		case "/api/networks/9/map-directories":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"NETWORK_MAP_DIRECTORIES is off for your organization"}`)
		case "/api/networks/7/map-directories":
			_, _ = io.WriteString(w, `{"directories":[{"path":"/","mapIds":["4"]},{"path":"/prod","mapIds":["3"],"createdById":"12","createdAt":"2026-10-01T10:00:00Z","createdBy":"mary"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// A map row carries id, name, pinned and the create/update attribution, and a network with no maps is an empty non-nil list, not an error.
func TestNetworkMapsList(t *testing.T) {
	t.Parallel()
	var uris []string
	maps := newTestClient(t, mapsServer(t, &uris).URL).NetworkMaps
	ctx := context.Background()

	rows, _, err := maps.List(ctx, "7")
	if err != nil || len(rows) != 2 {
		t.Fatalf("List = %+v, %v", rows, err)
	}
	core := rows[0]
	if core.ID != "3" || core.Name != "Core" || core.Pinned == nil || !*core.Pinned || core.CreatedBy != "mary" || core.CreatedByID != "12" ||
		core.CreatedAt != "2026-10-01T10:00:00Z" || core.UpdatedBy != "joe" || core.UpdatedAt != "2026-10-02T10:00:00Z" {
		t.Fatalf("row = %+v", core)
	}
	if rows[1].Pinned != nil {
		t.Fatalf("a map whose pinned was never set must read nil, got %v", *rows[1].Pinned)
	}
	empty, _, err := maps.List(ctx, "8")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("no maps = %#v, %v; want an empty non-nil list and no error", empty, err)
	}
	if uris[0] != "GET /api/networks/7/maps" {
		t.Fatalf("uri = %q", uris[0])
	}
}

// A caller without VIEW_NETWORK_MAPS gets the typed 403 carrying the missing permission.
func TestNetworkMapsRefusalIsTyped(t *testing.T) {
	t.Parallel()
	var uris []string
	_, _, err := newTestClient(t, mapsServer(t, &uris).URL).NetworkMaps.List(context.Background(), "9")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
	if op, ok := MissingPermission(err); !ok || op != "NetworkOperation.VIEW_NETWORK_MAPS" {
		t.Fatalf("MissingPermission = %q, %v", op, ok)
	}
}

// The stored map lists the devices placed on it (its coverage); the snapshot view lists only those the snapshot has, so the difference
// is what a snapshot lacks. The two views and the directory form are one route told apart by the query.
func TestNetworkMapsGetAndSnapshotCoverage(t *testing.T) {
	t.Parallel()
	var uris []string
	maps := newTestClient(t, mapsServer(t, &uris).URL).NetworkMaps
	ctx := context.Background()

	stored, _, err := maps.Get(ctx, "7", "3")
	if err != nil || stored.Name != "Core" || len(stored.Devices) != 3 || stored.Devices[0].X == nil || *stored.Devices[0].X != 1.5 || stored.Devices[1].X != nil {
		t.Fatalf("Get = %+v, %v", stored, err)
	}
	if stored.Directory != "" || string(stored.DeviceDefaults) != `{"label":"name"}` {
		t.Fatalf("Get must not claim a directory it did not ask for, and keeps raw settings: %+v", stored)
	}
	withDirectory, _, err := maps.GetWithDirectory(ctx, "7", "3")
	if err != nil || withDirectory.Directory != "/prod" {
		t.Fatalf("GetWithDirectory = %+v, %v", withDirectory, err)
	}
	snapshot, _, err := maps.Snapshot(ctx, "7", "3", "s9")
	if err != nil || len(snapshot.Devices) != 2 || snapshot.Devices[0].Vendor != "CISCO" || snapshot.Devices[0].CollectionIP != "10.0.0.1" || snapshot.Devices[0].ManagementIPs[0] != "10.0.0.1" ||
		strings.Join(snapshot.Devices[0].Tags, ",") != "core,dc1" || snapshot.Devices[0].LocationID != "L7" {
		t.Fatalf("Snapshot = %+v, %v", snapshot, err)
	}
	if names := strings.Join(snapshot.DeviceNames(), ","); names != "Core-1,core-2" {
		t.Fatalf("DeviceNames = %q", names)
	}
	missing := map[string]bool{}
	for _, d := range stored.Devices {
		missing[strings.ToLower(d.Name)] = true
	}
	for _, name := range snapshot.DeviceNames() {
		delete(missing, strings.ToLower(name))
	}
	if len(missing) != 1 || !missing["gone-9"] {
		t.Fatalf("devices the snapshot lacks = %v, want gone-9", missing)
	}
	want := []string{
		"GET /api/networks/7/maps/3",
		"GET /api/networks/7/maps/3?with=directory",
		"GET /api/networks/7/maps/3?snapshotId=s9&view=snapshot",
	}
	if strings.Join(uris, "|") != strings.Join(want, "|") {
		t.Fatalf("uris = %v, want %v", uris, want)
	}
}

// Omitting the snapshot sends no snapshotId, so the appserver applies its own default.
func TestNetworkMapsSnapshotScopedReadsOmitAnEmptySnapshot(t *testing.T) {
	t.Parallel()
	var uris []string
	maps := newTestClient(t, mapsServer(t, &uris).URL).NetworkMaps
	ctx := context.Background()

	if _, _, err := maps.Snapshot(ctx, "7", "3", " "); err != nil {
		t.Fatal(err)
	}
	structures, _, err := maps.Structures(ctx, "7", "3", "")
	if err != nil || len(structures) != 1 || structures[0].Type != "SPINE_LEAF" || len(structures[0].Structures) != 1 || structures[0].Structures[0].Devices[0] != "Core-2" {
		t.Fatalf("Structures = %+v, %v", structures, err)
	}
	if _, _, err := maps.Structures(ctx, "7", "3", "s9"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /api/networks/7/maps/3?view=snapshot",
		"GET /api/networks/7/maps/3/structures",
		"GET /api/networks/7/maps/3/structures?snapshotId=s9",
	}
	if strings.Join(uris, "|") != strings.Join(want, "|") {
		t.Fatalf("uris = %v, want %v", uris, want)
	}
}

// The directory listing has the root first; ForNetwork clients pass an empty network id; a blank map id is refused before a request.
func TestNetworkMapsDirectoriesBoundNetworkAndRefusals(t *testing.T) {
	t.Parallel()
	var uris []string
	server := mapsServer(t, &uris)
	bound := newTestClient(t, server.URL).ForNetwork("7").NetworkMaps
	ctx := context.Background()

	dirs, _, err := bound.Directories(ctx, "")
	if err != nil || len(dirs.Directories) != 2 || dirs.Directories[0].Path != "/" || dirs.Directories[0].CreatedBy != "" ||
		dirs.Directories[1].Path != "/prod" || dirs.Directories[1].MapIDs[0] != "3" || dirs.Directories[1].CreatedBy != "mary" {
		t.Fatalf("Directories = %+v, %v", dirs, err)
	}
	before := len(uris)
	for _, call := range []func() error{
		func() error { _, _, err := bound.Get(ctx, "", " "); return err },
		func() error { _, _, err := bound.Snapshot(ctx, "", "", "s1"); return err },
		func() error { _, _, err := bound.Structures(ctx, "", "", ""); return err },
		func() error { _, _, err := newTestClient(t, server.URL).NetworkMaps.List(ctx, ""); return err },
	} {
		if call() == nil {
			t.Fatal("a call with no map id (or no network) was accepted")
		}
	}
	if len(uris) != before {
		t.Fatalf("a refused call reached the wire: %v", uris[before:])
	}
	// A map id is one path segment.
	if _, _, err := bound.Get(ctx, "", "a/b"); err == nil {
		t.Fatal("expected the server's 404 for an escaped id")
	}
	if last := uris[len(uris)-1]; last != "GET /api/networks/7/maps/a%2Fb" {
		t.Fatalf("uri = %q", last)
	}
}

// A network with no maps still has the root directory; the directory switch being off is the feature-gated refusal, not a permission one.
func TestNetworkMapsDirectoriesEmptyAndGated(t *testing.T) {
	t.Parallel()
	var uris []string
	maps := newTestClient(t, mapsServer(t, &uris).URL).NetworkMaps
	ctx := context.Background()

	dirs, _, err := maps.Directories(ctx, "8")
	if err != nil || len(dirs.Directories) != 1 || dirs.Directories[0].Path != "/" || len(dirs.Directories[0].MapIDs) != 0 {
		t.Fatalf("Directories on a network with no maps = %+v, %v", dirs, err)
	}
	_, _, err = maps.Directories(ctx, "9")
	if !errors.Is(err, ErrFeatureGated) || errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrFeatureGated and not ErrPermissionDenied", err)
	}
}
