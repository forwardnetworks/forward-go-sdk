package domain

import (
	"errors"
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
	// KeyColumns is a composite key for a result no single column identifies (BGP sessions are unique per device, VRF and neighbor).
	// When set it replaces KeyColumn: a row's key is the tuple of these columns' values, the check that no key repeats and none is
	// missing runs on the tuple, and the sort is on the FIRST column only (Forward sorts by one column), so rows tied on it are
	// not ordered stably; a boundary repeat that creates is caught by the tuple check and retried like any other inconsistency.
	KeyColumns []string
	// NonUniqueKey says the key columns group rows rather than identify them (an ACL entry row is keyed by its device and ACL
	// name, and an ACL has many entries). The sort still uses the first key column, but the repeated-key check is OFF, so the
	// guarantee is only that the total is stable across pages and the rows read equal it: a repeated row that hides a missing one
	// keeps the count and is NOT caught. Use it only when no column is unique, and prefer a unique key when there is one.
	NonUniqueKey bool
	// MaxRows stops a runaway read: an error when the total exceeds it. Zero
	// means one million; every row is held in memory as a map, so size it to the
	// memory you have. It cannot exceed what an int32 offset can address.
	MaxRows int64
	// Attempts is how many whole passes RunAll makes before giving up with
	// ErrNQEPagingInconsistent: three when zero.
	Attempts int
}
