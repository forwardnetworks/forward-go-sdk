package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// AIAssistService accesses Forward's NQE and Predict assistance APIs.
//
// Preview: these endpoints are unpublished and controlled by AI_ALLOWED and,
// for Predict assistance, PREDICT_AI_ASSIST and PREDICT_MODELING.
type AIAssistService service

// NQEQueryAssist is an AI-generated NQE query and its validation state.
type NQEQueryAssist struct {
	ID         Identifier `json:"id"`
	Query      string     `json:"query"`
	ValidQuery bool       `json:"validQuery"`
}

// NQESummaryAssist is an AI-generated summary of an NQE query.
type NQESummaryAssist struct {
	ID      Identifier `json:"id"`
	Summary string     `json:"summary"`
}

// NQEDocMessage is prior user or assistant context for a documentation assist.
type NQEDocMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// NQEDocReference identifies documentation used in an answer.
type NQEDocReference struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// NQEDocAssist is an AI-generated answer grounded in NQE documentation.
type NQEDocAssist struct {
	ID         Identifier        `json:"id"`
	Answer     string            `json:"answer"`
	References []NQEDocReference `json:"references"`
}

// NQEDocAssistRequest asks an NQE documentation question.
type NQEDocAssistRequest struct {
	Question         string          `json:"question"`
	PreviousMessages []NQEDocMessage `json:"previousMessages,omitempty"`
}

// PredictOverviewAssist contains generated change-set metadata.
type PredictOverviewAssist struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// GenerateNQEQuery turns a natural-language prompt into NQE source.
func (s *AIAssistService) GenerateNQEQuery(
	ctx context.Context,
	prompt string,
) (*NQEQueryAssist, *Response, error) {
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
	assist := new(NQEQueryAssist)
	resp, err := s.client.Do(req, assist)
	if err != nil {
		return nil, resp, err
	}
	return assist, resp, nil
}

// SummarizeNQEQuery generates a natural-language description of NQE source.
func (s *AIAssistService) SummarizeNQEQuery(
	ctx context.Context,
	query string,
) (*NQESummaryAssist, *Response, error) {
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
	assist := new(NQESummaryAssist)
	resp, err := s.client.Do(req, assist)
	if err != nil {
		return nil, resp, err
	}
	return assist, resp, nil
}

// AskNQEDocs asks a question grounded in Forward's NQE documentation.
func (s *AIAssistService) AskNQEDocs(
	ctx context.Context,
	request NQEDocAssistRequest,
) (*NQEDocAssist, *Response, error) {
	if strings.TrimSpace(request.Question) == "" {
		return nil, nil, errors.New("forward: NQE documentation question is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/nqe/doc-assists", request)
	if err != nil {
		return nil, nil, err
	}
	assist := new(NQEDocAssist)
	resp, err := s.client.Do(req, assist)
	if err != nil {
		return nil, resp, err
	}
	return assist, resp, nil
}

// SuggestNQEDocFollowups returns suggested questions for an existing answer.
func (s *AIAssistService) SuggestNQEDocFollowups(
	ctx context.Context,
	assistID string,
	answer string,
) ([]string, *Response, error) {
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
func (s *AIAssistService) GeneratePredictOverview(
	ctx context.Context,
	networkID string,
	changeSetID string,
) (*PredictOverviewAssist, *Response, error) {
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
	assist := new(PredictOverviewAssist)
	resp, err := s.client.Do(req, assist)
	if err != nil {
		return nil, resp, err
	}
	return assist, resp, nil
}

// GeneratePredictCLI generates device-specific CLI for a change-set intent.
func (s *AIAssistService) GeneratePredictCLI(
	ctx context.Context,
	networkID string,
	changeSetID string,
	deviceName string,
	prompt string,
) (string, *Response, error) {
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
func (s *AIAssistService) SummarizeConfigDiff(
	ctx context.Context,
	beforeSnapshotID string,
	afterSnapshotID string,
) (string, *Response, error) {
	return s.summarizeDiff(ctx, beforeSnapshotID, afterSnapshotID, "config-summary-assists")
}

// SummarizeChangeImpact generates an AI summary of a snapshot's modeled impact.
func (s *AIAssistService) SummarizeChangeImpact(
	ctx context.Context,
	beforeSnapshotID string,
	afterSnapshotID string,
) (string, *Response, error) {
	return s.summarizeDiff(ctx, beforeSnapshotID, afterSnapshotID, "impact-summary-assists")
}

func (s *AIAssistService) summarizeDiff(
	ctx context.Context,
	beforeSnapshotID string,
	afterSnapshotID string,
	operation string,
) (string, *Response, error) {
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
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
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
