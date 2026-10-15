package forward

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
)

// ErrNQEPagingInconsistent means RunAll's pages did not add up to one coherent
// result in any attempt: the total changed between pages, the pages held fewer
// rows than the total, or a key repeated. The rows read are discarded rather
// than returned as if they were the whole answer.
var ErrNQEPagingInconsistent = errors.New("forward: NQE pages did not form one consistent result")

// NQEPagedOptions controls RunAll.
type NQEPagedOptions struct {
	// PageSize is the rows per request: 1000 when zero, at most 10000 (Forward's cap).
	PageSize int32
	// KeyColumn is REQUIRED: a column whose value is unique per row (a device
	// name, say). RunAll sorts by it when the query has no sort of its own, and
	// checks that no value repeats across the pages and that none is missing
	// from a row. Forward documents no row order across offset/limit pages, and
	// a sort on a column with ties is not stable either: rows on a page boundary
	// can repeat while others go missing with the total unchanged, which only a
	// unique key can catch.
	KeyColumn string
	// MaxRows stops a runaway read: an error when the total exceeds it. Zero
	// means one million; every row is held in memory as a map, so size it to the
	// memory you have. It cannot exceed what an int32 offset can address.
	MaxRows int64
	// Attempts is how many whole passes RunAll makes before giving up with
	// ErrNQEPagingInconsistent: three when zero.
	Attempts int
}

// RunAll reads every row of a query across offset/limit pages and returns them
// only if they form one consistent whole. A single offset/limit read of a large
// result has no consistency of its own: pages are separate queries, and a
// 41,000-device network was seen to return passes missing about 550 rows while
// every page still said the same total. RunAll defends in three ways: it needs
// a KeyColumn (an error otherwise); it pins the snapshot the first page
// answered for, so a newer snapshot landing mid-read cannot mix pages; and it
// checks that the pages share one total, that the rows read equal it, and that
// no key repeats, repeating the pass (up to Attempts) and then failing with
// ErrNQEPagingInconsistent rather than returning a partial set. It is a plain
// sequence of Run calls, so a result that is only big (not unstable) costs one
// request per page.
func (s *NQEService) RunAll(ctx context.Context, networkID, snapshotID string, query NQEQueryRequest, options NQEPagedOptions) ([]NQERecord, *Response, error) {
	pageSize := options.PageSize
	if pageSize <= 0 {
		pageSize = 1000
	}
	if pageSize > 10000 {
		return nil, nil, errors.New("forward: NQE page size cannot exceed 10000")
	}
	maxRows := options.MaxRows
	if maxRows <= 0 {
		maxRows = 1_000_000
	}
	if maxRows > math.MaxInt32 {
		return nil, nil, errors.New("forward: MaxRows cannot exceed what an int32 offset addresses")
	}
	attempts := options.Attempts
	if attempts <= 0 {
		attempts = 3
	}
	if strings.TrimSpace(options.KeyColumn) == "" {
		return nil, nil, errors.New("forward: RunAll needs a KeyColumn, a column unique per row: without one only the row count can be checked, and a repeated row that hides a missing one keeps the count")
	}
	// The key is the sort, unless the query states its own (which should then
	// still order ties by the key).
	sortBy := &NQESortOrder{ColumnName: options.KeyColumn, Order: "ASC"}
	if query.Options != nil && query.Options.SortBy != nil {
		sortBy = query.Options.SortBy
	}

	var lastProblem error
	var resp *Response
	for attempt := 0; attempt < attempts; attempt++ {
		rows, r, problem, err := s.runAllOnce(ctx, networkID, snapshotID, query, sortBy, pageSize, maxRows, options.KeyColumn)
		if err != nil {
			return nil, r, err
		}
		resp = r
		if problem == nil {
			return rows, resp, nil
		}
		lastProblem = problem
	}
	return nil, resp, fmt.Errorf("%w after %d attempts: %v", ErrNQEPagingInconsistent, attempts, lastProblem)
}

// runAllOnce makes one pass. A non-nil problem is an inconsistency to retry; err
// is a failure to give up on.
func (s *NQEService) runAllOnce(ctx context.Context, networkID, snapshotID string, query NQEQueryRequest, sortBy *NQESortOrder, pageSize int32, maxRows int64, keyColumn string) (rows []NQERecord, resp *Response, problem, err error) {
	pinned := snapshotID
	var total int64 = -1
	seen := map[string]struct{}{}
	// The offset advances by the rows RECEIVED, not the page size: Forward can
	// return a short page when the rows would exceed its page size cap.
	for offset := int32(0); ; offset = int32(len(rows)) {
		page := query
		options := NQEOptions{}
		if query.Options != nil {
			options = *query.Options
		}
		options.SortBy, options.Offset, options.Limit = sortBy, &offset, &pageSize
		page.Options = &options
		result, r, err := s.Run(ctx, networkID, pinned, page)
		if err != nil {
			return nil, r, nil, err
		}
		resp = r
		if pinned == "" {
			pinned = string(result.SnapshotID)
			if pinned == "" {
				return nil, resp, nil, errors.New("forward: the NQE response names no snapshot, so later pages cannot be pinned to it")
			}
		}
		if total < 0 {
			total = result.TotalNumItems
			if total > maxRows {
				return nil, resp, nil, fmt.Errorf("forward: the query has %d rows, over the MaxRows limit of %d", total, maxRows)
			}
		} else if result.TotalNumItems != total {
			return nil, resp, fmt.Errorf("the total changed from %d to %d at offset %d", total, result.TotalNumItems, offset), nil
		}
		if keyColumn != "" {
			for _, item := range result.Items {
				raw, present := item[keyColumn]
				if !present || len(raw) == 0 || string(raw) == "null" {
					return nil, resp, nil, fmt.Errorf("forward: a row has no %q column, so RunAll cannot check keys; KeyColumn must name a column the query selects", keyColumn)
				}
				key := string(raw)
				if _, dup := seen[key]; dup {
					return nil, resp, fmt.Errorf("key %s appeared twice (page at offset %d)", key, offset), nil
				}
				seen[key] = struct{}{}
			}
		}
		rows = append(rows, result.Items...)
		if int64(len(rows)) >= total || len(result.Items) == 0 {
			break
		}
		if int64(len(rows)) > math.MaxInt32 {
			return nil, resp, nil, errors.New("forward: more rows than an int32 offset can address")
		}
	}
	if int64(len(rows)) != total {
		return nil, resp, fmt.Errorf("read %d rows but the total is %d", len(rows), total), nil
	}
	return rows, resp, nil, nil
}
