package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type NQERepository service

func (s *NQERepository) Head(ctx context.Context) (ports.Identifier, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/nqe/repos/org/commits/head", nil)
	if err != nil {
		return "", nil, err
	}
	req = markOperation(req, "NQERepository.Head")
	var out ports.Identifier
	response, err := s.client.doRequired(req, &out)
	if err == nil && out == "" {
		err = errors.New("forward: NQE head returned no commit ID")
	}
	return out, response, err
}

func (s *NQERepository) ListHeadQueries(ctx context.Context) ([]ports.NQECommittedQueryInfo, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var result struct {
		Queries []ports.NQECommittedQueryInfo `json:"queries"`
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/nqe/repos/org/commits/head/queries", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "NQERepository.ListHeadQueries")
	response, err := s.client.doRequired(req, &result)
	return result.Queries, response, err
}

func (s *NQERepository) GetQuery(ctx context.Context, commitID, path string) (*ports.NQECommittedQuery, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	commitID, path = strings.TrimSpace(commitID), strings.TrimSpace(path)
	if commitID == "" || path == "" {
		return nil, nil, errors.New("forward: NQE commit ID and path are required")
	}
	query := url.Values{"path": []string{path}, "with": []string{"sourceCode"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/nqe/repos/org/commits/"+url.PathEscape(commitID)+"/queries?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "NQERepository.GetQuery")
	out := new(ports.NQECommittedQuery)
	response, err := s.client.doRequired(req, out)
	if err == nil && strings.TrimSpace(out.SourceCode) == "" {
		err = ports.ErrNQEEmptySource
	}
	return out, response, err
}

// GetQueryByID reads a query's source by its STABLE query id ("Q_<sha>" for
// the org repository, "FQ_<sha>" for the Forward Library) at an immutable
// commit: GET /api/nqe/queries/{queryId}/source-code?commitId=.
// NqeLibController.getQuerySourceCode, mapped at primary 15398425a69 and
// stable 67e89c87124. The id survives a query being moved or renamed, which
// a path does not; GetQuery is the by-path read. A committed query with empty
// source is ErrNQEEmptySource, never an empty success.
func (s *NQERepository) GetQueryByID(ctx context.Context, commitID, queryID string) (*ports.NQEQuerySourceAtCommit, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	commitID, queryID = strings.TrimSpace(commitID), strings.TrimSpace(queryID)
	if commitID == "" || queryID == "" {
		return nil, nil, errors.New("forward: NQE commit ID and query ID are required")
	}
	query := url.Values{"commitId": []string{commitID}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/nqe/queries/"+url.PathEscape(queryID)+"/source-code?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "NQERepository.GetQueryByID")
	out := new(ports.NQEQuerySourceAtCommit)
	response, err := s.client.doRequired(req, out)
	if err == nil && strings.TrimSpace(out.SourceCode) == "" {
		err = ports.ErrNQEEmptySource
	}
	return out, response, err
}

func (s *NQERepository) DeleteDirectory(ctx context.Context, path string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.change(ctx, "deleteDir", path, nil, http.StatusNotFound, http.StatusConflict)
}

func (s *NQERepository) AddDirectory(ctx context.Context, path string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.change(ctx, "addDir", path, nil, http.StatusConflict)
}

// AddQuery stages a NEW query at path in the caller's workspace (a draft until Commit). It is only for a path that is not in HEAD:
// Forward refuses an existing path with 409, reason ADD_QUERY_ALREADY_EXISTS ("Attempted to add a query at <path>, but that path
// already exists in HEAD."). Change an existing query with EditQuery. Calling AddQuery again on its own draft replaces that draft.
func (s *NQERepository) AddQuery(ctx context.Context, path, source string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(source) == "" {
		return nil, errors.New("forward: NQE query source is required")
	}
	return s.change(ctx, "addQuery", path, ports.NQEQuerySource{SourceCode: source})
}

// DeleteQuery stages the deletion of a query in the caller's workspace. Like every other change here it is a draft: Commit on the same
// path is what removes it from the org library, and DiscardChange drops the staged deletion instead. A 404 is tolerated so deleting
// what is already gone succeeds.
func (s *NQERepository) DeleteQuery(ctx context.Context, path string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.change(ctx, "deleteQuery", path, nil, http.StatusNotFound)
}

func (s *NQERepository) change(ctx context.Context, action, path string, payload any, accepted ...int) (*ports.Response, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("forward: NQE change path is required")
	}
	query := url.Values{"action": []string{action}, "path": []string{path}}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/users/current/nqe/changes?"+query.Encode(), payload)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "NQERepository."+action)
	response, err := s.client.Do(req, nil)
	for _, status := range accepted {
		if ports.IsStatus(err, status) {
			return response, nil
		}
	}
	return response, err
}

func (s *NQERepository) Commit(ctx context.Context, input ports.NQECommitRequest) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	input.Paths = normalizeNQEPaths(input.Paths)
	if len(input.Paths) == 0 {
		return nil, errors.New("forward: NQE commit requires a path")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/nqe/repos/org/commits", input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "NQERepository.Commit")
	return s.client.Do(req, nil)
}

func normalizeNQEPaths(paths []string) []string {
	out := normalizeStrings(paths)
	sort.Strings(out)
	return out
}

// History returns the commits that touched a query, by its stable ID ("Q_..."
// or "FQ_..."). GET /api/nqe/queries/{queryId}/history (NqeLibController; on
// primary 15398425a69 and stable 67e89c87124). Preview: not in the published
// spec.
func (s *NQERepository) History(ctx context.Context, queryID string) ([]ports.NQEQueryCommit, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if queryID = strings.TrimSpace(queryID); queryID == "" {
		return nil, nil, errors.New("forward: NQE query ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/nqe/queries/"+url.PathEscape(queryID)+"/history", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "NQERepository.History")
	result := listResponse[ports.NQEQueryCommit]{Keys: []string{"commits"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// CommitDryRun reports what committing the staged changes at paths would do,
// without committing. With a snapshotID the queries are also typed against
// that snapshot's data model; without one, only against the library. POST
// /api/nqe/repos/org/commits?dryRun=true[&snapshotId=] (NqeLibController;
// on primary 15398425a69 and stable 67e89c87124). Preview: not in the
// published spec.
func (s *NQERepository) CommitDryRun(ctx context.Context, paths []string, snapshotID string) (*ports.NQECommitDryRun, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	paths = normalizeNQEPaths(paths)
	if len(paths) == 0 {
		return nil, nil, errors.New("forward: at least one NQE path is required")
	}
	query := url.Values{"dryRun": []string{"true"}}
	setString(query, "snapshotId", snapshotID)
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/nqe/repos/org/commits?"+query.Encode(), map[string][]string{"paths": paths})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "NQERepository.CommitDryRun")
	out := new(ports.NQECommitDryRun)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// EditQuery stages new source for a query that is already in HEAD (or renamed from one) in the caller's workspace, adding or
// replacing the draft at path; Commit publishes it and DiscardChange drops it. POST /api/users/current/nqe/changes?action=editQuery
// &path= {sourceCode, basis} (NqeLibController.editQueryDraft; WRITE_TO_NQE_LIBRARY). A path not in HEAD is refused with 409
// (PATH_MISSING_IN_HEAD or UPDATE_EXISTING_FOR_NEW_QUERY): stage a new query with AddQuery. Preview: not in the published spec.
func (s *NQERepository) EditQuery(ctx context.Context, path, source string, basis ports.NQEDraftBasis) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(source) == "" {
		return nil, errors.New("forward: NQE query source is required")
	}
	basis.QueryID, basis.CommitID = strings.TrimSpace(basis.QueryID), strings.TrimSpace(basis.CommitID)
	if basis.QueryID == "" || basis.CommitID == "" {
		return nil, errors.New("forward: an NQE edit needs its basis query ID and commit ID")
	}
	return s.change(ctx, "editQuery", path, struct {
		SourceCode string              `json:"sourceCode"`
		Basis      ports.NQEDraftBasis `json:"basis"`
	}{source, basis})
}

// DiscardChange drops the caller's draft at exactly path -- a staged add, edit or deletion of a query, or a directory's staged access
// change -- leaving HEAD untouched. Having no draft there is success, so cleanup can always run. DELETE
// /api/users/current/nqe/changes?path= (NqeLibController.discardChanges; WRITE_TO_NQE_LIBRARY). Preview.
func (s *NQERepository) DiscardChange(ctx context.Context, path string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if path = strings.TrimSpace(path); path == "" {
		return nil, errors.New("forward: NQE change path is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/users/current/nqe/changes?"+url.Values{"path": []string{path}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "NQERepository.DiscardChange")
	response, err := s.client.Do(req, nil)
	// NqeLibStore.discardChanges: "no change at the path" is NqeLibException INVALID_CHANGE_PATH, rendered as 409.
	var apiErr *ports.ErrorResponse
	if ports.IsStatus(err, http.StatusConflict) && errors.As(err, &apiErr) && apiErr.Reason == "INVALID_CHANGE_PATH" {
		return response, nil
	}
	return response, err
}

// DiscardChanges drops every draft of the caller's at or under paths (query paths, or directories for everything inside them);
// paths with no draft are ignored. POST /api/users/current/nqe/changes?action=bulkDiscard {paths}
// (NqeLibController.discardChangesRecursiveBulk; WRITE_TO_NQE_LIBRARY). Preview.
func (s *NQERepository) DiscardChanges(ctx context.Context, paths []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	paths = normalizeNQEPaths(paths)
	if len(paths) == 0 {
		return nil, errors.New("forward: at least one NQE path is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/users/current/nqe/changes?action=bulkDiscard", map[string][]string{"paths": paths})
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "NQERepository.DiscardChanges")
	return s.client.Do(req, nil)
}

// ListDrafts returns the caller's uncommitted NQE changes. GET /api/users/current/nqe/changes (getUserWorkspaceChanges;
// VIEW_NQE_LIBRARY). Preview.
func (s *NQERepository) ListDrafts(ctx context.Context) ([]ports.NQEDraftChange, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users/current/nqe/changes", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "NQERepository.ListDrafts")
	result := listResponse[ports.NQEDraftChange]{Keys: []string{"changes"}}
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// GetDraft returns the caller's draft of the query at path, or (nil, nil) when there is none ("No draft query at <path>"). GET
// /api/users/current/nqe/changes?path= (getQueryDraftDetails; VIEW_NQE_LIBRARY). Preview.
func (s *NQERepository) GetDraft(ctx context.Context, path string) (*ports.NQEDraftQuery, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if path = strings.TrimSpace(path); path == "" {
		return nil, nil, errors.New("forward: NQE query path is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users/current/nqe/changes?"+url.Values{"path": []string{path}}.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "NQERepository.GetDraft")
	out := new(ports.NQEDraftQuery)
	response, err := s.client.doRequired(req, out)
	if ports.IsStatus(err, http.StatusNotFound) {
		return nil, response, nil
	}
	if err != nil {
		return nil, response, err
	}
	return out, response, nil
}
