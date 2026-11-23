package rest

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// WebhooksService manages organization-scoped outbound callbacks.
//
// Preview: webhook schemas are unpublished and deployment capability-gated.
type Webhooks service

// List returns configured webhooks and the last test result for each, keyed by
// webhook name.
func (s *Webhooks) List(ctx context.Context) ([]ports.Webhook, map[string]ports.WebhookTestResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, nil, err
	}
	var envelope struct {
		Webhooks    []ports.Webhook                    `json:"webhooks"`
		TestResults map[string]ports.WebhookTestResult `json:"testResults"`
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/webhooks", nil)
	if err != nil {
		return nil, nil, nil, err
	}
	resp, err := s.client.Do(req, &envelope)
	return envelope.Webhooks, envelope.TestResults, resp, err
}

func (s *Webhooks) Create(ctx context.Context, request ports.WebhookRequest) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := validateWebhook(request); err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/webhooks", request)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

func (s *Webhooks) Update(ctx context.Context, name string, patch ports.WebhookPatch) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := webhookPath(name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

func (s *Webhooks) Delete(ctx context.Context, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := webhookPath(name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// TestNew tests a webhook without persisting it and returns the dynamic test result.
func (s *Webhooks) TestNew(ctx context.Context, request ports.WebhookRequest) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := validateWebhook(request); err != nil {
		return nil, nil, err
	}
	var result json.RawMessage
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/webhooks?action=test", request)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &result)
	return result, resp, err
}

func webhookPath(name string) (string, error) {
	if name = strings.TrimSpace(name); name == "" {
		return "", errors.New("forward: webhook name is required")
	}
	return "/api/webhooks/" + url.PathEscape(name), nil
}

func validateWebhook(request ports.WebhookRequest) error {
	if strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.URL) == "" {
		return errors.New("forward: webhook name and URL are required")
	}
	return nil
}
