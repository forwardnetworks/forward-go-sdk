package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// WebhooksService manages organization-scoped outbound callbacks.
//
// Preview: webhook schemas are unpublished and deployment capability-gated.
type WebhooksService service

// WebhookCredentialRequest is an outbound webhook credential. Secret values
// are write-only and never included in client lifecycle events.
type WebhookCredentialRequest struct {
	Type     string `json:"type"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// WebhookRequest creates or reconciles a webhook. EventParams and Template
// remain raw because their shapes depend on the event type and Forward version.
type WebhookRequest struct {
	Name                 string                    `json:"name"`
	Description          string                    `json:"description,omitempty"`
	URL                  string                    `json:"url"`
	DisableSSLValidation bool                      `json:"disableSslValidation"`
	EventParams          map[string]any            `json:"eventParams"`
	Credential           *WebhookCredentialRequest `json:"credential,omitempty"`
	Template             json.RawMessage           `json:"template,omitempty"`
	Enabled              *bool                     `json:"enabled,omitempty"`
}

// Webhook retains the complete response to tolerate schema drift.
type Webhook struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description,omitempty"`
	URL         string                     `json:"url"`
	Enabled     bool                       `json:"enabled"`
	Raw         map[string]json.RawMessage `json:"-"`
}

func (w *Webhook) UnmarshalJSON(data []byte) error {
	type plain Webhook
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*w = Webhook(value)
	w.Raw = raw
	return nil
}

// List returns configured webhooks. TestResults retains the version-specific
// test status object keyed by webhook name.
func (s *WebhooksService) List(ctx context.Context) ([]Webhook, map[string]json.RawMessage, *Response, error) {
	var envelope struct {
		Webhooks    []Webhook                  `json:"webhooks"`
		TestResults map[string]json.RawMessage `json:"testResults"`
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/webhooks", nil)
	if err != nil {
		return nil, nil, nil, err
	}
	resp, err := s.client.Do(req, &envelope)
	return envelope.Webhooks, envelope.TestResults, resp, err
}

func (s *WebhooksService) Create(ctx context.Context, request WebhookRequest) (*Response, error) {
	if err := validateWebhook(request); err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/webhooks", request)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// Update patches an existing webhook. patch may include explicit JSON nulls.
func (s *WebhooksService) Update(ctx context.Context, name string, patch map[string]any) (*Response, error) {
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

func (s *WebhooksService) Delete(ctx context.Context, name string) (*Response, error) {
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
func (s *WebhooksService) TestNew(ctx context.Context, request WebhookRequest) (json.RawMessage, *Response, error) {
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

func validateWebhook(request WebhookRequest) error {
	if strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.URL) == "" {
		return errors.New("forward: webhook name and URL are required")
	}
	return nil
}
