package rest

// ChangeReportsService builds a ChangeReport by reading Forward. The report's shape and assembly rules are shared with forward-python-sdk
// (testdata/change_report/RULES.md); this file is the Go side of the reads, which follow the "Reads" rules there: one existing read per section,
// sequential, each with its own time budget, and a section that cannot be read becomes an unavailable section with a typed reason, never a failed
// report and never a silent "no change". Preview: composed only from reads this SDK already has; nothing here was run against a live Forward.
type ChangeReports service
