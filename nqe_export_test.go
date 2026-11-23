package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// pbLen encodes one length-delimited protobuf field. It is written here, from the wire format and Forward's schema (NqeLibExportPB { repeated
// ExportedQueryPB queries = 1 }, ExportedQueryPB { QueryPathPB path = 1; NqeQuerySourcePB source_code = 2 }, both wrappers one string
// field 1), so the test does not reuse the SDK's decoder to build its input.
func pbLen(field byte, payload []byte) []byte {
	out := []byte{field<<3 | 2}
	n := len(payload)
	for n >= 0x80 {
		out = append(out, byte(n)|0x80)
		n >>= 7
	}
	out = append(out, byte(n))
	return append(out, payload...)
}

func exportedQuery(path, source string) []byte {
	entry := append(pbLen(1, pbLen(1, []byte(path))), pbLen(2, pbLen(1, []byte(source)))...)
	return pbLen(1, entry)
}

func exportZip(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(payload)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// One request replaces a GET per query: the paths go in the body, the answer is a ZIP of Forward's protobuf export, and every query's
// path and source come back, including a source long enough to need a two-byte length.
func TestExportSources(t *testing.T) {
	long := string(bytes.Repeat([]byte("select {x: 1};\n"), 40)) // 600 bytes: a two-byte varint length
	payload := append(exportedQuery("/autocon6/stale_bgp_advertisements", "foreach d in network.devices select {Device: d.name}"),
		exportedQuery("/q2", long)...)
	var gotPath, gotQuery, gotAccept string
	var gotBody map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotAccept = r.URL.Path, r.URL.RawQuery, r.Header.Get("Accept")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(exportZip(t, "queries-export.proto", payload))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	sources, _, err := client.NQERepository.ExportOrgSources(context.Background(), []string{"/autocon6/", "/q2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources["/autocon6/stale_bgp_advertisements"] != "foreach d in network.devices select {Device: d.name}" || sources["/q2"] != long {
		t.Fatalf("sources = %v", sources)
	}
	if gotPath != "/api/users/current/nqe/changes" || gotQuery != "action=export" || gotAccept != "application/zip" || len(gotBody["paths"]) != 2 || gotBody["paths"][0] != "/autocon6/" {
		t.Fatalf("sent %s?%s accept=%q body=%v", gotPath, gotQuery, gotAccept, gotBody)
	}
	if _, _, err = client.NQERepository.ExportForwardSources(context.Background(), []string{"/"}); err != nil || gotPath != "/api/nqe/repos/fwd/commits/head/queries" {
		t.Fatalf("forward export: path=%s err=%v", gotPath, err)
	}
	if got, _, err := client.NQERepository.ExportOrgSources(context.Background(), nil); err != nil || len(got) != 0 {
		t.Fatalf("no paths must read nothing without a request: %v %v", got, err)
	}
	if _, _, err = client.NQERepository.ExportOrgSources(context.Background(), []string{"relative"}); err == nil {
		t.Fatal("a path that does not start with / must be refused before any request")
	}
}

// A ZIP without Forward's entry, and one that is not a ZIP at all, are errors, never an empty success.
func TestExportSourcesRefusesWrongAnswers(t *testing.T) {
	answer := exportZip(t, "other.txt", []byte("x"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(answer) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	if _, _, err := client.NQERepository.ExportOrgSources(context.Background(), []string{"/"}); err == nil {
		t.Fatal("a ZIP with no queries-export.proto must be an error")
	}
	answer = []byte("not a zip")
	if _, _, err := client.NQERepository.ExportOrgSources(context.Background(), []string{"/"}); err == nil {
		t.Fatal("a body that is not a ZIP must be an error")
	}
}

// QUERY_EDIT drafts carry no path (Forward's ChangeInfo.QueryEdit has only directory, name and basis): QueryPath must name the query from
// the basis, with a move or rename applied.
func TestNQEDraftQueryPath(t *testing.T) {
	basis := &domain.NQEChangeBasis{Path: "/team/stale_bgp"}
	cases := []struct {
		name  string
		draft domain.NQEDraftChange
		want  string
	}{
		{"add", domain.NQEDraftChange{Type: "QUERY_ADD", Path: "/new/q"}, "/new/q"},
		{"edit in place", domain.NQEDraftChange{Type: "QUERY_EDIT", Basis: basis}, "/team/stale_bgp"},
		{"edit and rename", domain.NQEDraftChange{Type: "QUERY_EDIT", Basis: basis, Name: "stale_bgp_v2"}, "/team/stale_bgp_v2"},
		{"edit and move", domain.NQEDraftChange{Type: "QUERY_EDIT", Basis: basis, Directory: "/other"}, "/other/stale_bgp"},
		{"edit, move and rename", domain.NQEDraftChange{Type: "QUERY_EDIT", Basis: basis, Directory: "/other/", Name: "x"}, "/other/x"},
		{"delete", domain.NQEDraftChange{Type: "QUERY_DELETE", Basis: basis}, "/team/stale_bgp"},
		{"permission change", domain.NQEDraftChange{Type: "PERMISSION_SET", Directory: "/team/"}, ""},
	}
	for _, tc := range cases {
		if got := tc.draft.QueryPath(); got != tc.want {
			t.Errorf("%s: QueryPath = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A draft concerns a path when the path is its Path, where it ends up, or the committed path it started from: the last catches an edit that
// renames the query away from the path, whose draft has no Path.
func TestNQEDraftTouches(t *testing.T) {
	renamed := domain.NQEDraftChange{Type: "QUERY_EDIT", Basis: &domain.NQEChangeBasis{Path: "/team/q"}, Name: "q2"}
	if !renamed.Touches("/team/q") || !renamed.Touches("/team/q2") || renamed.Touches("/team/other") || renamed.Touches("") {
		t.Fatalf("a rename must touch both the old and the new path: %+v", renamed)
	}
	if !(domain.NQEDraftChange{Type: "QUERY_ADD", Path: "/new/q"}).Touches("/new/q") {
		t.Fatal("an add touches its Path")
	}
}
