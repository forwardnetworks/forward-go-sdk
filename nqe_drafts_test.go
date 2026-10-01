package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// An existing library query can only be changed with action=editQuery and a
// basis (NqeQueryEditRequest: sourceCode plus basis{queryId, commitId}, both
// required); AddQuery on its path is a 409 ADD_QUERY_ALREADY_EXISTS.
func TestNQERepositoryEditQuery(t *testing.T) {
	t.Parallel()

	var method, uri, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, uri, body = r.Method, r.URL.RequestURI(), string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	repo := newTestClient(t, server.URL).NQERepository

	if _, err := repo.EditQuery(context.Background(), "/L3/MTU check", "foreach d in network.devices select {name: d.name}", NQEDraftBasis{QueryID: " Q_abc ", CommitID: "c42"}); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || uri != "/api/users/current/nqe/changes?action=editQuery&path=%2FL3%2FMTU+check" {
		t.Fatalf("sent %s %s", method, uri)
	}
	if body != `{"sourceCode":"foreach d in network.devices select {name: d.name}","basis":{"queryId":"Q_abc","commitId":"c42"}}` {
		t.Fatalf("body = %s", body)
	}
	for _, basis := range []NQEDraftBasis{{QueryID: "Q_abc"}, {CommitID: "c42"}} {
		if _, err := repo.EditQuery(context.Background(), "/q", "x", basis); err == nil {
			t.Fatalf("basis %+v must be refused locally: Forward requires both fields", basis)
		}
	}
}

// Cleanup must always be able to run: discarding a path with no draft is
// Forward's 409 INVALID_CHANGE_PATH and reads as success, while any other
// conflict still surfaces.
func TestNQERepositoryDiscard(t *testing.T) {
	t.Parallel()

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		switch r.URL.Query().Get("path") {
		case "/none":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"message":"User has no changes at the following paths: /none.","reason":"INVALID_CHANGE_PATH"}`)
		case "/other":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"message":"something else","reason":"INVALID_DIR_PATH"}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	repo := newTestClient(t, server.URL).NQERepository
	ctx := context.Background()

	if _, err := repo.DiscardChange(ctx, "/q"); err != nil || calls[0] != "DELETE /api/users/current/nqe/changes?path=%2Fq " {
		t.Fatalf("DiscardChange: %v %q", err, calls[0])
	}
	if _, err := repo.DiscardChange(ctx, "/none"); err != nil {
		t.Fatalf("no draft to discard must be success: %v", err)
	}
	if _, err := repo.DiscardChange(ctx, "/other"); !IsStatus(err, http.StatusConflict) {
		t.Fatalf("another conflict must surface: %v", err)
	}
	if _, err := repo.DiscardChanges(ctx, []string{"/b", " ", "/a"}); err != nil || calls[3] != `POST /api/users/current/nqe/changes?action=bulkDiscard {"paths":["/a","/b"]}` {
		t.Fatalf("DiscardChanges: %v %q", err, calls[3])
	}
	if _, err := repo.DiscardChanges(ctx, nil); err == nil {
		t.Fatal("an empty path list must be refused")
	}
}

func TestNQERepositoryDrafts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/api/users/current/nqe/changes":
			_, _ = io.WriteString(w, `{"changes":[{"type":"QUERY_ADD","path":"/new","sourceCodeSha":"s1"},
			  {"type":"QUERY_EDIT","sourceCodeSha":"s2","basis":{"queryId":"Q_1","commitId":"c42","path":"/old"}},
			  {"type":"QUERY_DELETE","basis":{"queryId":"Q_2","commitId":"c42","path":"/gone"}}]}`)
		case "/api/users/current/nqe/changes?path=%2Fnew":
			_, _ = io.WriteString(w, `{"sourceCode":"select 1","sourceCodeSha":"s1","lastUpdated":1759312800000}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"No draft query at /missing"}`)
		}
	}))
	defer server.Close()
	repo := newTestClient(t, server.URL).NQERepository
	ctx := context.Background()

	drafts, _, err := repo.ListDrafts(ctx)
	if err != nil || len(drafts) != 3 || drafts[0].Path != "/new" || drafts[1].Basis.Path != "/old" || drafts[2].Type != "QUERY_DELETE" {
		t.Fatalf("ListDrafts() = %+v, %v", drafts, err)
	}
	draft, _, err := repo.GetDraft(ctx, "/new")
	if err != nil || draft.SourceCode != "select 1" || draft.LastUpdatedMillis == nil {
		t.Fatalf("GetDraft() = %+v, %v", draft, err)
	}
	if draft, _, err := repo.GetDraft(ctx, "/missing"); draft != nil || err != nil {
		t.Fatalf("absent GetDraft() = %+v, %v", draft, err)
	}
}
