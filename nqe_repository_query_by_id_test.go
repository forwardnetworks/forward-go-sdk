package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNQERepositoryGetQueryByID(t *testing.T) {
	t.Parallel()

	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		switch {
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/nqe/queries/Q_abc/source-code":
			if got := r.URL.Query().Get("commitId"); got != "c0ffee" {
				t.Errorf("commitId = %q, want c0ffee", got)
			}
			if r.URL.Query().Has("path") || r.URL.Query().Has("with") {
				t.Errorf("the by-id read must not send path/with: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"sourceCode":"/** @intent up */\nforeach d in network.devices select {n: d.name}","intent":"up","sourceCodeSha":"x"}`)
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/nqe/queries/Q_empty/source-code":
			_, _ = io.WriteString(w, `{"sourceCode":"  "}`)
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/nqe/queries/Q_gone/source-code":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"No query with 'queryId' Q_gone found at commit c0ffee"}`)
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/nqe/queries/Q_a%2Fb/source-code":
			_, _ = io.WriteString(w, `{"sourceCode":"select 1"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()

	q, _, err := client.NQERepository.GetQueryByID(ctx, " c0ffee ", " Q_abc ")
	if err != nil {
		t.Fatalf("GetQueryByID() error = %v", err)
	}
	if q.Intent != "up" || q.SourceCode == "" {
		t.Fatalf("GetQueryByID() = %#v", q)
	}

	if _, _, err := client.NQERepository.GetQueryByID(ctx, "c0ffee", "Q_empty"); !errors.Is(err, ErrNQEEmptySource) {
		t.Fatalf("empty source: err = %v, want ErrNQEEmptySource", err)
	}
	if _, _, err := client.NQERepository.GetQueryByID(ctx, "c0ffee", "Q_gone"); !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("missing query: err = %v, want a 404", err)
	}
	// An id is a path segment: it is escaped, never spliced.
	if _, _, err := client.NQERepository.GetQueryByID(ctx, "c0ffee", "Q_a/b"); err != nil {
		t.Fatalf("escaped id: %v", err)
	}
	before := len(seen)
	for _, args := range [][2]string{{"", "Q_abc"}, {"c0ffee", ""}, {" ", " "}} {
		if _, _, err := client.NQERepository.GetQueryByID(ctx, args[0], args[1]); err == nil {
			t.Fatalf("GetQueryByID(%q, %q) accepted a missing argument", args[0], args[1])
		}
	}
	if len(seen) != before {
		t.Fatalf("a missing argument still sent a request: %v", seen[before:])
	}
}
