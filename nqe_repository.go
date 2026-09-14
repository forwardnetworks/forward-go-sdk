package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type NQERepositoryService service

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

func (s *NQERepositoryService) Head(ctx context.Context) (Identifier, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/nqe/repos/org/commits/head", nil)
	if err != nil {
		return "", nil, err
	}
	req = markOperation(req, "NQERepository.Head")
	var out Identifier
	response, err := s.client.doRequired(req, &out)
	if err == nil && out == "" {
		err = errors.New("forward: NQE head returned no commit ID")
	}
	return out, response, err
}

func (s *NQERepositoryService) ListHeadQueries(ctx context.Context) ([]NQECommittedQueryInfo, *Response, error) {
	var result struct {
		Queries []NQECommittedQueryInfo `json:"queries"`
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/nqe/repos/org/commits/head/queries", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "NQERepository.ListHeadQueries")
	response, err := s.client.doRequired(req, &result)
	return result.Queries, response, err
}

func (s *NQERepositoryService) GetQuery(ctx context.Context, commitID, path string) (*NQECommittedQuery, *Response, error) {
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
	out := new(NQECommittedQuery)
	response, err := s.client.doRequired(req, out)
	if err == nil && strings.TrimSpace(out.SourceCode) == "" {
		err = ErrNQEEmptySource
	}
	return out, response, err
}

func (s *NQERepositoryService) DeleteDirectory(ctx context.Context, path string) (*Response, error) {
	return s.change(ctx, "deleteDir", path, nil, http.StatusNotFound, http.StatusConflict)
}
func (s *NQERepositoryService) AddDirectory(ctx context.Context, path string) (*Response, error) {
	return s.change(ctx, "addDir", path, nil, http.StatusConflict)
}
func (s *NQERepositoryService) AddQuery(ctx context.Context, path, source string) (*Response, error) {
	if strings.TrimSpace(source) == "" {
		return nil, errors.New("forward: NQE query source is required")
	}
	return s.change(ctx, "addQuery", path, NQEQuerySource{SourceCode: source})
}

// DeleteQuery removes a query in the caller's workspace. Like every other
// change here it is a draft: Commit on the same path is what removes it from
// the org library. A 404 is tolerated so deleting what is already gone
// succeeds.
func (s *NQERepositoryService) DeleteQuery(ctx context.Context, path string) (*Response, error) {
	return s.change(ctx, "deleteQuery", path, nil, http.StatusNotFound)
}

func (s *NQERepositoryService) change(ctx context.Context, action, path string, payload any, accepted ...int) (*Response, error) {
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
		if isStatus(err, status) {
			return response, nil
		}
	}
	return response, err
}

func (s *NQERepositoryService) Commit(ctx context.Context, input NQECommitRequest) (*Response, error) {
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
