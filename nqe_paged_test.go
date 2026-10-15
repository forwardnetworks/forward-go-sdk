package forward

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"context"
)

// pagedServer answers NQE pages over rows "d000".."d(n-1)" sorted by name.
// flaky makes the first `flakyPasses` passes lose a row on the page boundary
// and repeat another one, the way an unstable offset/limit read behaves, while
// every page still reports the same total.
func pagedServer(t *testing.T, n int, flakyPasses int32, queries *[]string) *httptest.Server {
	var passes atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Options struct {
				Offset int `json:"offset"`
				Limit  int `json:"limit"`
				SortBy struct {
					ColumnName string `json:"columnName"`
				} `json:"sortBy"`
			} `json:"queryOptions"`
		}
		_ = json.Unmarshal(raw, &body)
		*queries = append(*queries, r.URL.RawQuery+" sort="+body.Options.SortBy.ColumnName)
		if body.Options.Offset == 0 {
			passes.Add(1)
		}
		flaky := passes.Load() <= flakyPasses
		var items []string
		for i := body.Options.Offset; i < n && i < body.Options.Offset+body.Options.Limit; i++ {
			row := i
			if flaky && i == body.Options.Offset+body.Options.Limit-1 && body.Options.Offset > 0 {
				row = i - 1 // a repeat, and the true row is missing
			}
			items = append(items, fmt.Sprintf(`{"name":"d%03d"}`, row))
		}
		_, _ = fmt.Fprintf(w, `{"snapshotId":"snap-7","totalNumItems":%d,"items":[%s]}`, n, strings.Join(items, ","))
	}))
}

// RunAll pins the snapshot the first page answered for, sorts by the key, and
// returns the rows only when they add up; an unstable pass is retried; one
// that never settles is an error, never a partial set.
func TestRunAllReturnsAConsistentSetOrFailsLoudly(t *testing.T) {
	ctx := context.Background()
	query := NQEQueryRequest{Query: "foreach d in network.devices select {name: d.name}"}
	options := NQEPagedOptions{PageSize: 10, KeyColumn: "name", Attempts: 3}

	var queries []string
	steady := pagedServer(t, 25, 0, &queries)
	defer steady.Close()
	rows, _, err := newTestClient(t, steady.URL).NQE.RunAll(ctx, "net-1", "", query, options)
	if err != nil || len(rows) != 25 {
		t.Fatalf("rows = %d, %v", len(rows), err)
	}
	if len(queries) != 3 || !strings.Contains(queries[0], "networkId=net-1") || strings.Contains(queries[0], "snapshotId") ||
		!strings.Contains(queries[1], "snapshotId=snap-7") || !strings.HasSuffix(queries[2], "sort=name") {
		t.Fatalf("the first page asks the network, later pages pin the answering snapshot and sort by the key: %q", queries)
	}

	queries = nil
	flaky := pagedServer(t, 25, 1, &queries)
	defer flaky.Close()
	rows, _, err = newTestClient(t, flaky.URL).NQE.RunAll(ctx, "net-1", "", query, options)
	if err != nil || len(rows) != 25 {
		t.Fatalf("an unstable first pass must be retried: rows=%d, %v", len(rows), err)
	}

	queries = nil
	broken := pagedServer(t, 25, 99, &queries)
	defer broken.Close()
	rows, _, err = newTestClient(t, broken.URL).NQE.RunAll(ctx, "net-1", "", query, options)
	if !errors.Is(err, ErrNQEPagingInconsistent) || rows != nil {
		t.Fatalf("a result that never settles must be an error with no rows: rows=%d, %v", len(rows), err)
	}

	if _, _, err = newTestClient(t, steady.URL).NQE.RunAll(ctx, "net-1", "", query, NQEPagedOptions{}); err == nil {
		t.Fatal("no key must be refused before any request")
	}
	explicit := query
	explicit.Options = &NQEOptions{SortBy: &NQESortOrder{ColumnName: "name"}}
	if _, _, err = newTestClient(t, steady.URL).NQE.RunAll(ctx, "net-1", "", explicit, NQEPagedOptions{PageSize: 100}); err == nil {
		t.Fatal("a sort without a key must be refused: only the count could be checked")
	}
	if _, _, err = newTestClient(t, steady.URL).NQE.RunAll(ctx, "net-1", "", query, NQEPagedOptions{PageSize: 100, KeyColumn: "nope"}); err == nil || errors.Is(err, ErrNQEPagingInconsistent) {
		t.Fatalf("a key the rows do not carry is a hard error, not an inconsistency to retry: %v", err)
	}
}

// Forward can return a page shorter than the limit (its page size cap), so the
// next offset must be the rows received, or every pass loses the rows after a
// short page and the failure is blamed on instability.
func TestRunAllAdvancesByRowsReceivedAfterAShortPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Options struct {
				Offset int `json:"offset"`
				Limit  int `json:"limit"`
			} `json:"queryOptions"`
		}
		_ = json.Unmarshal(raw, &body)
		limit := min(body.Options.Limit, 4) // the server never returns more than 4 rows
		var items []string
		for i := body.Options.Offset; i < 10 && i < body.Options.Offset+limit; i++ {
			items = append(items, fmt.Sprintf(`{"name":"d%03d"}`, i))
		}
		_, _ = fmt.Fprintf(w, `{"snapshotId":"s","totalNumItems":10,"items":[%s]}`, strings.Join(items, ","))
	}))
	defer server.Close()
	rows, _, err := newTestClient(t, server.URL).NQE.RunAll(context.Background(), "n", "", NQEQueryRequest{Query: "q"}, NQEPagedOptions{PageSize: 8, KeyColumn: "name"})
	if err != nil || len(rows) != 10 {
		t.Fatalf("short pages must not lose rows: %d, %v", len(rows), err)
	}
}
