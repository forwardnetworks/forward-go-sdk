package domain

import (
	"encoding/json"
	"errors"
	"strings"
)

var ErrNQEEmptySource = errors.New("forward: NQE query returned empty source")

type NQECommittedQueryInfo struct {
	Path         string     `json:"path"`
	LastCommitID Identifier `json:"lastCommitId"`
	QueryID      Identifier `json:"queryId,omitempty"`
}

type NQECommittedQuery struct {
	SourceCode string     `json:"sourceCode"`
	QueryID    Identifier `json:"queryId,omitempty"`
}

type NQECommitRequest struct {
	Paths   []string          `json:"paths"`
	Message *NQECommitMessage `json:"message,omitempty"`
}

type NQECommitMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type NQEQuerySource struct {
	SourceCode string `json:"sourceCode"`
}

// NQEQuerySourceAtCommit is a committed query's source and doc-comment
// metadata as Forward returns it from /api/nqe/queries/{queryId}/source-code
// (com.forwardnetworks.cv.nqe.lib.NqeQuery: sourceCode plus the unwrapped
// doc comment's intent/description).
type NQEQuerySourceAtCommit struct {
	SourceCode  string `json:"sourceCode"`
	Intent      string `json:"intent,omitempty"`
	Description string `json:"description,omitempty"`
}

// NQEQueryCommit is one commit that touched a query (QueryCommitInfo): the
// path the query had at that commit, the commit ID, its author (AuthorID for a
// user, AuthorEmail otherwise; Author is a display name when Forward has one),
// time and message.
type NQEQueryCommit struct {
	Path        string     `json:"path"`
	ID          Identifier `json:"id"`
	AuthorID    Identifier `json:"authorId,omitempty"`
	AuthorEmail string     `json:"authorEmail,omitempty"`
	Author      string     `json:"author,omitempty"`
	CommittedAt string     `json:"committedAt,omitempty"`
	Title       string     `json:"title,omitempty"`
	Body        string     `json:"body,omitempty"`
}

// NQEDiagnostic is one compiler diagnostic. Location stays raw: it is a text
// region (source name, start and end positions) that varies in which parts it
// carries.
type NQEDiagnostic struct {
	Severity string          `json:"severity,omitempty"`
	Source   string          `json:"source,omitempty"`
	Message  string          `json:"message"`
	Location json.RawMessage `json:"location,omitempty"`
}

// NQECommitDryRun is what committing the staged paths would do
// (CommitDryRunInfo). NewErrors maps a query path to the diagnostics the
// commit would introduce there -- including in queries that import the
// changed ones. Uses are the checks, dashboards and other consumers of the
// changed queries, kept raw. The Unauthorized lists name changes the caller
// may not commit.
type NQECommitDryRun struct {
	NewErrors                        map[string][]NQEDiagnostic `json:"newErrors"`
	Uses                             []json.RawMessage          `json:"uses,omitempty"`
	UnauthorizedQueryChanges         []string                   `json:"unauthorizedQueryChanges,omitempty"`
	UnauthorizedAccessSettingChanges []string                   `json:"unauthorizedAccessSettingChanges,omitempty"`
}

// NQEDraftBasis is the committed version an edit starts from: the query's stable ID ("Q_...", NQECommittedQuery.QueryID) and a commit
// that holds the query at that path, normally the head commit read alongside it (Head, GetQuery). Forward rejects the edit (409) when
// the basis does not match: a commit without the query, a query ID that differs, or a basis older than a draft already in place.
type NQEDraftBasis struct {
	QueryID  string `json:"queryId"`
	CommitID string `json:"commitId"`
}

// NQEDraftChange is one draft in the caller's workspace (ChangeInfo). Type is QUERY_ADD, QUERY_EDIT, QUERY_DELETE, DIR_ADD,
// PERMISSION_SET or PERMISSION_RESET. Path is set for an ADD (the query or directory path) and NOT for an edit or a delete: Forward's
// ChangeInfo.QueryEdit and QueryDelete carry no path field, only Basis (the committed query with its path at that commit), so Path is
// empty for a QUERY_EDIT draft and QueryPath is the way to name it. An edit also sets Directory when it moves the query or Name when it
// renames it, and SourceCodeSha when it changes the source; Directory alone names a permission change.
type NQEDraftChange struct {
	Type          string          `json:"type"`
	Path          string          `json:"path,omitempty"`
	Directory     string          `json:"directory,omitempty"`
	Name          string          `json:"name,omitempty"`
	SourceCodeSha string          `json:"sourceCodeSha,omitempty"`
	Basis         *NQEChangeBasis `json:"basis,omitempty"`
}

// QueryPath is the path this draft's query is known by: Path for an add; for an edit, where the edit leaves the query (the committed
// path with Directory and Name applied when it moves or renames it); for a delete, the committed path. Empty for a directory or
// permission draft, whose Path or Directory is the thing named. Source-derived: ChangeInfo.QueryEdit has directory (when moved), name
// (when renamed) and basis; it has not been read from a live response.
func (d NQEDraftChange) QueryPath() string {
	if d.Path != "" {
		return d.Path
	}
	if d.Basis == nil || d.Basis.Path == "" {
		return ""
	}
	if d.Type != "QUERY_EDIT" || (d.Directory == "" && d.Name == "") {
		return d.Basis.Path
	}
	dir, name := d.Basis.Path[:strings.LastIndexByte(d.Basis.Path, '/')+1], d.Basis.Path[strings.LastIndexByte(d.Basis.Path, '/')+1:]
	if d.Directory != "" {
		dir = d.Directory
		if !strings.HasSuffix(dir, "/") {
			dir += "/"
		}
	}
	if d.Name != "" {
		name = d.Name
	}
	return dir + name
}

// Touches reports whether this draft concerns the query or directory at path: its Path, its QueryPath, or the committed path it was based on
// (Basis.Path). The last matters for an edit that renames or moves the query away from path, whose draft carries no Path at all; a check for
// "someone else's draft is on this path" that reads only Path misses the most common clash (edits and deletes).
func (d NQEDraftChange) Touches(path string) bool {
	if path == "" {
		return false
	}
	return d.Path == path || d.QueryPath() == path || (d.Basis != nil && d.Basis.Path == path)
}

// NQEChangeBasis is the committed query a draft started from: its stable ID, the commit, and its path in that commit.
type NQEChangeBasis struct {
	QueryID  Identifier `json:"queryId"`
	CommitID Identifier `json:"commitId"`
	Path     string     `json:"path"`
}

// NQEDraftQuery is the caller's draft source for one query (QueryDraftInfo). LastUpdatedMillis is epoch milliseconds.
type NQEDraftQuery struct {
	SourceCode        string `json:"sourceCode"`
	SourceCodeSha     string `json:"sourceCodeSha,omitempty"`
	LastUpdatedMillis *int64 `json:"lastUpdated,omitempty"`
}
