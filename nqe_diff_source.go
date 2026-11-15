package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// DiffNQERows compares two result sets of the same query row by row, matching rows on keyColumns, and returns the rows that were
// ADDED (only after), DELETED (only before) or MODIFIED (same key, different values), in key order, as Forward's NQE diff
// returns them. UNCHANGED rows are left out. Values are compared as decoded JSON, so the order of a row's columns does not matter.
// A key that repeats within one side is an error: a diff keyed on a non-unique column would pair the wrong rows. A row missing a
// key column is an error too.
func DiffNQERows(before, after []NQERecord, keyColumns ...string) ([]NQEDiffEntry, error) {
	if len(keyColumns) == 0 {
		return nil, errors.New("forward: an NQE row diff needs at least one key column")
	}
	index := func(rows []NQERecord, side string) (map[string]NQERecord, error) {
		out := make(map[string]NQERecord, len(rows))
		for _, row := range rows {
			parts := make([]string, len(keyColumns))
			for i, column := range keyColumns {
				raw, ok := row[column]
				if !ok {
					return nil, fmt.Errorf("forward: a %s row has no %q column to key the diff on", side, column)
				}
				parts[i] = string(bytes.TrimSpace(raw))
			}
			key := strings.Join(parts, "\x00")
			if _, dup := out[key]; dup {
				return nil, fmt.Errorf("forward: the %s rows repeat the key %s: the key columns are not unique", side, strings.Join(parts, ", "))
			}
			out[key] = row
		}
		return out, nil
	}
	b, err := index(before, "before")
	if err != nil {
		return nil, err
	}
	a, err := index(after, "after")
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(a)+len(b))
	for key := range b {
		keys = append(keys, key)
	}
	for key := range a {
		if _, both := b[key]; !both {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var out []NQEDiffEntry
	for _, key := range keys {
		old, hadBefore := b[key]
		now, hasAfter := a[key]
		switch {
		case hadBefore && !hasAfter:
			row := old
			out = append(out, NQEDiffEntry{Type: "DELETED", Before: &row})
		case !hadBefore && hasAfter:
			row := now
			out = append(out, NQEDiffEntry{Type: "ADDED", After: &row})
		default:
			same, err := sameNQERow(old, now)
			if err != nil {
				return nil, err
			}
			if !same {
				oldRow, nowRow := old, now
				out = append(out, NQEDiffEntry{Type: "MODIFIED", Before: &oldRow, After: &nowRow})
			}
		}
	}
	return out, nil
}

func sameNQERow(a, b NQERecord) (bool, error) {
	if len(a) != len(b) {
		return false, nil
	}
	for column, rawA := range a {
		rawB, ok := b[column]
		if !ok {
			return false, nil
		}
		if bytes.Equal(rawA, rawB) {
			continue
		}
		var x, y any
		if err := json.Unmarshal(rawA, &x); err != nil {
			return false, fmt.Errorf("forward: decode NQE column %q: %w", column, err)
		}
		if err := json.Unmarshal(rawB, &y); err != nil {
			return false, fmt.Errorf("forward: decode NQE column %q: %w", column, err)
		}
		xa, _ := json.Marshal(x)
		yb, _ := json.Marshal(y)
		if !bytes.Equal(xa, yb) {
			return false, nil
		}
	}
	return true, nil
}

// DiffSource runs one query (inline source or a library QueryID, with its Parameters) on both snapshots with RunAll and diffs the
// rows by options.KeyColumn. Forward's own POST /api/nqe-diffs/{before}/{after} takes only a library queryId (NqeDiffRequest has no
// source field), so a query written in source, or one that needs parameters Forward's diff does not take, is diffed here instead; this
// reads every row of both snapshots (one request per page) and compares in memory, so size options.MaxRows to what you can hold. Both
// reads keep RunAll's consistency checks, and a read that cannot be made consistent fails rather than diffing a partial set.
func (s *NQEService) DiffSource(ctx context.Context, networkID, beforeSnapshotID, afterSnapshotID string, query NQEQueryRequest, options NQEPagedOptions) ([]NQEDiffEntry, *Response, error) {
	before, resp, err := s.RunAll(ctx, networkID, beforeSnapshotID, query, options)
	if err != nil {
		return nil, resp, err
	}
	after, resp, err := s.RunAll(ctx, networkID, afterSnapshotID, query, options)
	if err != nil {
		return nil, resp, err
	}
	diff, err := DiffNQERows(before, after, options.KeyColumn)
	return diff, resp, err
}
