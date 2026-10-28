package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type predictSeen struct{ method, uri, body, contentType string }

// predictRecorder answers every request with the given status and body and records what was sent.
func predictRecorder(t *testing.T, status int, reply string, seen *[]predictSeen) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*seen = append(*seen, predictSeen{r.Method, r.URL.RequestURI(), string(b), r.Header.Get("Content-Type")})
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(server.Close)
	return server
}

// The directory tree reads as path to change-set ids, and every directory action sends its action and arguments on the
// directory's path, where the path travels as ONE escaped segment (the GUI encodeURIComponent's it; Forward allows encoded slashes).
func TestChangeSetDirectoriesWire(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	predict := newTestClient(t, predictRecorder(t, 200, `{"/prod":["2","3"],"/prod/wave":["1"]}`, &seen).URL).Predict
	ctx := context.Background()

	dirs, _, err := predict.ListChangeSetDirectories(ctx, "7")
	if err != nil || len(dirs) != 2 || len(dirs["/prod"]) != 2 || dirs["/prod"][1] != "3" || dirs["/prod/wave"][0] != "1" {
		t.Fatalf("dirs = %v, %v", dirs, err)
	}
	seen = nil
	steps := []struct {
		call func() (*Response, error)
		want string
		body string
	}{
		{func() (*Response, error) { return predict.AddChangeSetDirectory(ctx, "7", "/prod", "wave 1") }, "POST /api/networks/7/change-set-directories/%2Fprod?action=addDir&name=wave+1", ""},
		{func() (*Response, error) { return predict.RenameChangeSetDirectory(ctx, "7", "/prod/wave 1", "wave-1") }, "POST /api/networks/7/change-set-directories/%2Fprod%2Fwave%201?action=rename&name=wave-1", ""},
		{func() (*Response, error) { return predict.MoveChangeSetDirectory(ctx, "7", "/prod/wave-1", "/") }, "POST /api/networks/7/change-set-directories/%2Fprod%2Fwave-1?action=move&to=%2F", ""},
		{func() (*Response, error) { return predict.MoveChangeSetToDirectory(ctx, "7", "/prod", "9") }, "POST /api/networks/7/change-set-directories/%2Fprod?action=moveChangeSetTo&changeSetId=9", ""},
		{func() (*Response, error) {
			return predict.MoveChangeSetsToDirectory(ctx, "7", "/", []string{"4", " 5 ", "4", ""})
		}, "POST /api/networks/7/change-set-directories/%2F?action=moveChangeSetsTo", `{"changeSetIds":["4","5"]}`},
		{func() (*Response, error) { return predict.DeleteChangeSetDirectory(ctx, "7", "/prod", false) }, "DELETE /api/networks/7/change-set-directories/%2Fprod", ""},
		{func() (*Response, error) { return predict.DeleteChangeSetDirectory(ctx, "7", "/prod", true) }, "DELETE /api/networks/7/change-set-directories/%2Fprod?retainChangeSets=false", ""},
	}
	for _, step := range steps {
		seen = nil
		if _, err := step.call(); err != nil {
			t.Fatalf("%s: %v", step.want, err)
		}
		if len(seen) != 1 || seen[0].method+" "+seen[0].uri != step.want || seen[0].body != step.body {
			t.Fatalf("sent %+v, want %s body %q", seen, step.want, step.body)
		}
	}
}

// Paths Forward would answer 400 (relative, trailing or doubled slash, the root where it cannot apply) and empty ids never reach the wire.
func TestChangeSetDirectoryRefusals(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	predict := newTestClient(t, predictRecorder(t, 204, ``, &seen).URL).Predict
	ctx := context.Background()
	for name, err := range map[string]error{
		"delete root":        func() error { _, err := predict.DeleteChangeSetDirectory(ctx, "7", "/", true); return err }(),
		"relative path":      func() error { _, err := predict.DeleteChangeSetDirectory(ctx, "7", "prod", false); return err }(),
		"trailing slash":     func() error { _, err := predict.AddChangeSetDirectory(ctx, "7", "/prod/", "x"); return err }(),
		"name with slash":    func() error { _, err := predict.AddChangeSetDirectory(ctx, "7", "/", "a/b"); return err }(),
		"blank name":         func() error { _, err := predict.RenameChangeSetDirectory(ctx, "7", "/a", " "); return err }(),
		"rename root":        func() error { _, err := predict.RenameChangeSetDirectory(ctx, "7", "/", "x"); return err }(),
		"move root":          func() error { _, err := predict.MoveChangeSetDirectory(ctx, "7", "/", "/a"); return err }(),
		"move to relative":   func() error { _, err := predict.MoveChangeSetDirectory(ctx, "7", "/a", "b"); return err }(),
		"no change set":      func() error { _, err := predict.MoveChangeSetToDirectory(ctx, "7", "/a", " "); return err }(),
		"no change sets":     func() error { _, err := predict.MoveChangeSetsToDirectory(ctx, "7", "/a", []string{" "}); return err }(),
		"bulk delete no ids": func() error { _, err := predict.DeleteChangeSets(ctx, "7", nil); return err }(),
		"tags no names": func() error {
			_, err := predict.PatchChangeSetsTags(ctx, "7", []string{"1"}, ChangeSetTagsAdd, nil)
			return err
		}(),
		"remove all with name": func() error {
			_, err := predict.PatchChangeSetsTags(ctx, "7", []string{"1"}, ChangeSetTagsRemoveAll, []string{"x"})
			return err
		}(),
		"unknown tag action": func() error {
			_, err := predict.PatchChangeSetsTags(ctx, "7", []string{"1"}, "SET", []string{"x"})
			return err
		}(),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if len(seen) != 0 {
		t.Fatalf("a refused call reached the wire: %+v", seen)
	}
}

// Bulk delete and bulk tag edits send the documented bodies; create-in-directory sends dirPath as a query and no directory in the body.
func TestChangeSetBulkAndCreateInDirectoryWire(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	predict := newTestClient(t, predictRecorder(t, 200, `{"id":"11","name":"n","snapshotId":"s1"}`, &seen).URL).Predict
	ctx := context.Background()

	if _, err := predict.DeleteChangeSets(ctx, "7", []string{"1", "2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := predict.PatchChangeSetsTags(ctx, "7", []string{"1", "2"}, ChangeSetTagsAdd, []string{" wave1 ", "ops"}); err != nil {
		t.Fatal(err)
	}
	if _, err := predict.PatchChangeSetsTags(ctx, "7", []string{"1"}, ChangeSetTagsRemoveAll, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := predict.CreateChangeSet(ctx, "7", ChangeSetCreateRequest{Name: "n", SnapshotID: "s1", Directory: "/prod/wave 1"}); err != nil {
		t.Fatal(err)
	}
	want := []predictSeen{
		{"POST", "/api/networks/7/change-sets?action=delete", `{"changeSetIds":["1","2"]}`, "application/json"},
		{"PATCH", "/api/networks/7/change-sets", `{"changeSetIds":["1","2"],"update":{"tags":{"action":"ADD","names":["wave1","ops"]}}}`, "application/json"},
		{"PATCH", "/api/networks/7/change-sets", `{"changeSetIds":["1"],"update":{"tags":{"action":"REMOVE_ALL"}}}`, "application/json"},
		{"POST", "/api/networks/7/change-sets?dirPath=%2Fprod%2Fwave+1", `{"name":"n","snapshotId":"s1"}`, "application/json"},
	}
	if len(seen) != len(want) {
		t.Fatalf("seen %+v", seen)
	}
	for i, w := range want {
		if seen[i].method != w.method || seen[i].uri != w.uri || strings.TrimSpace(seen[i].body) != w.body || !strings.HasPrefix(seen[i].contentType, w.contentType) {
			t.Errorf("request %d = %+v, want %+v", i, seen[i], w)
		}
	}
}
