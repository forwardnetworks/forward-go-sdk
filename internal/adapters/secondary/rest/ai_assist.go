package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// AIAssistService accesses Forward's NQE and Predict assistance APIs.
//
// Preview: these endpoints are unpublished and controlled by AI_ALLOWED and,
// for Predict assistance, PREDICT_AI_ASSIST and PREDICT_MODELING (and PREDICT_FAILURE_SUMMARY_ASSIST for the failure summary).
type AIAssist service

// GenerateNQEQuery turns a natural-language prompt into NQE source.
func (s *AIAssist) GenerateNQEQuery(
	ctx context.Context,
	prompt string,
) (*ports.NQEQueryAssist, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if prompt = strings.TrimSpace(prompt); prompt == "" {
		return nil, nil, errors.New("forward: NQE assist prompt is required")
	}
	req, err := s.client.newJSONRequest(
		ctx,
		http.MethodPost,
		"/api/nqe/query-assists",
		map[string]string{"prompt": prompt},
	)
	if err != nil {
		return nil, nil, err
	}
	assist := new(ports.NQEQueryAssist)
	resp, err := s.client.Do(req, assist)
	if err != nil {
		return nil, resp, err
	}
	return assist, resp, nil
}

// SummarizeNQEQuery generates a natural-language description of NQE source.
func (s *AIAssist) SummarizeNQEQuery(
	ctx context.Context,
	query string,
) (*ports.NQESummaryAssist, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if query = strings.TrimSpace(query); query == "" {
		return nil, nil, errors.New("forward: NQE query is required")
	}
	req, err := s.client.newJSONRequest(
		ctx,
		http.MethodPost,
		"/api/nqe/summary-assists",
		map[string]string{"query": query},
	)
	if err != nil {
		return nil, nil, err
	}
	assist := new(ports.NQESummaryAssist)
	resp, err := s.client.Do(req, assist)
	if err != nil {
		return nil, resp, err
	}
	return assist, resp, nil
}

// AskNQEDocs asks a question grounded in Forward's NQE documentation.
func (s *AIAssist) AskNQEDocs(
	ctx context.Context,
	request ports.NQEDocAssistRequest,
) (*ports.NQEDocAssist, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(request.Question) == "" {
		return nil, nil, errors.New("forward: NQE documentation question is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/nqe/doc-assists", request)
	if err != nil {
		return nil, nil, err
	}
	assist := new(ports.NQEDocAssist)
	resp, err := s.client.Do(req, assist)
	if err != nil {
		return nil, resp, err
	}
	return assist, resp, nil
}

// SuggestNQEDocFollowups returns suggested questions for an existing answer.
func (s *AIAssist) SuggestNQEDocFollowups(
	ctx context.Context,
	assistID string,
	answer string,
) ([]string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	assistID = strings.TrimSpace(assistID)
	if assistID == "" || strings.TrimSpace(answer) == "" {
		return nil, nil, errors.New("forward: NQE doc assist ID and answer are required")
	}
	path := fmt.Sprintf("/api/nqe/doc-assists/%s/followup", url.PathEscape(assistID))
	req, err := s.client.newJSONRequest(
		ctx,
		http.MethodPost,
		path,
		map[string]string{"answer": answer},
	)
	if err != nil {
		return nil, nil, err
	}
	var result struct {
		Questions []string `json:"followupQuestions"`
	}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Questions, resp, nil
}

// GeneratePredictOverview generates a name, description, and tags for a change set.
func (s *AIAssist) GeneratePredictOverview(
	ctx context.Context,
	networkID string,
	changeSetID string,
) (*ports.PredictOverviewAssist, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := changeSetPath(networkID, changeSetID)
	if err != nil {
		return nil, nil, err
	}
	path += "/overview-assists"
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, struct{}{})
	if err != nil {
		return nil, nil, err
	}
	assist := new(ports.PredictOverviewAssist)
	resp, err := s.client.Do(req, assist)
	if err != nil {
		return nil, resp, err
	}
	return assist, resp, nil
}

// GeneratePredictCLI generates device-specific CLI for a change-set intent.
func (s *AIAssist) GeneratePredictCLI(
	ctx context.Context,
	networkID string,
	changeSetID string,
	deviceName string,
	prompt string,
) (string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", nil, err
	}
	path, err := changeSetPath(networkID, changeSetID)
	if err != nil {
		return "", nil, err
	}
	deviceName = strings.TrimSpace(deviceName)
	prompt = strings.TrimSpace(prompt)
	if deviceName == "" || prompt == "" {
		return "", nil, errors.New("forward: device name and Predict assist prompt are required")
	}
	path += fmt.Sprintf("/devices/%s/cli-assists", url.PathEscape(deviceName))
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, map[string]string{"prompt": prompt})
	if err != nil {
		return "", nil, err
	}
	var result struct {
		Commands string `json:"commands"`
	}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return "", resp, err
	}
	return result.Commands, resp, nil
}

// SummarizeConfigDiff generates an AI summary of configuration differences.
func (s *AIAssist) SummarizeConfigDiff(
	ctx context.Context,
	beforeSnapshotID string,
	afterSnapshotID string,
) (string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	return s.summarizeDiff(ctx, http.MethodPost, beforeSnapshotID, afterSnapshotID, "config-summary-assists")
}

// SummarizeChangeImpact generates an AI summary of a snapshot's modeled impact.
func (s *AIAssist) SummarizeChangeImpact(
	ctx context.Context,
	beforeSnapshotID string,
	afterSnapshotID string,
) (string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	return s.summarizeDiff(ctx, http.MethodPost, beforeSnapshotID, afterSnapshotID, "impact-summary-assists")
}

// SummarizeFailures generates an AI summary of what failed between two snapshots: regressions and new warnings the change introduced
// from the base to the predicted snapshot (GET /api/diffs/{a}/{b}/failure-summary-assist, PredictAssistController; VIEW_CHECK_DIFFS and
// the organization properties AI_ALLOWED and PREDICT_FAILURE_SUMMARY_ASSIST; preview). Predefined and intent checks and routing loops
// are always covered; NQE checks need USE_NQE, and new CVEs and newly exposed vulnerable devices need VIEW_CVE_DIFFS and a licence that
// allows it. A caller without those rights still gets an answer with those parts missing.
// Checks still processing do not block it: Forward then summarizes what is done, tells the model the counts are partial and does not
// cache the answer. No field reports that, only the text may say so, so call again later and compare rather than treating one answer as
// final. A complete summary is cached for an hour per user in each appserver's memory. Every call that is not a cache hit is a model
// call, and this is a GET, so a client with a retry policy resends it after a 502, a 504 or a transport failure: use WithoutRetry where
// that cost matters. The text comes from a language model: report it as such.
func (s *AIAssist) SummarizeFailures(
	ctx context.Context,
	beforeSnapshotID string,
	afterSnapshotID string,
) (string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	return s.summarizeDiff(ctx, http.MethodGet, beforeSnapshotID, afterSnapshotID, "failure-summary-assist")
}

func (s *AIAssist) summarizeDiff(
	ctx context.Context,
	method string,
	beforeSnapshotID string,
	afterSnapshotID string,
	operation string,
) (string, *ports.Response, error) {
	beforeSnapshotID = strings.TrimSpace(beforeSnapshotID)
	afterSnapshotID = strings.TrimSpace(afterSnapshotID)
	if beforeSnapshotID == "" || afterSnapshotID == "" {
		return "", nil, errors.New("forward: before and after snapshot IDs are required")
	}
	path := fmt.Sprintf(
		"/api/diffs/%s/%s/%s",
		url.PathEscape(beforeSnapshotID),
		url.PathEscape(afterSnapshotID),
		operation,
	)
	req, err := s.client.NewRequest(ctx, method, path, nil)
	if err != nil {
		return "", nil, err
	}
	var result struct {
		Summary string `json:"summary"`
	}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return "", resp, err
	}
	return result.Summary, resp, nil
}
