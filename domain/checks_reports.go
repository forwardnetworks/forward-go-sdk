package domain

import (
	"time"
)

// ChecksReportOptions bounds a report download. Forward builds the whole
// workbook before it sends the first byte, so a big snapshot takes a while.
// Timeout works as on SnapshotExportOptions: zero keeps the client's own (60
// seconds by default), positive replaces it, negative removes it.
type ChecksReportOptions struct {
	Timeout time.Duration
}
