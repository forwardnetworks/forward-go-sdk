package domain

import (
	"encoding/json"
)

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
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
	Enabled     bool   `json:"enabled"`
	// Raw carries fields this SDK version does not model, so an object read
	// from a newer appserver and written back does not silently lose them.
	Raw map[string]json.RawMessage `json:"-"`
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

// WebhookTestResult is the outcome of the last delivery test for one webhook.
type WebhookTestResult struct {
	Success    bool   `json:"success"`
	StatusCode *int   `json:"statusCode,omitempty"`
	Message    string `json:"message,omitempty"`
	TestedAt   string `json:"testedAt,omitempty"`
}

// Update patches an existing webhook.
// WebhookPatch changes part of a webhook. Pointer fields keep "not stated"
// distinct from "stated as empty" -- disabling a webhook and leaving its state
// alone are different requests. The fields mirror Forward's WebhookUpdate
// (WebhookController.updateWebhook, PATCH /webhooks/{webhookName}). Changing
// URL, DisableSSLValidation or Credential makes Forward re-test the connection
// before it answers. Credential replaces the stored one, so it needs both the
// username and the password.
type WebhookPatch struct {
	Name                 *string                   `json:"name,omitempty"`
	Description          *string                   `json:"description,omitempty"`
	URL                  *string                   `json:"url,omitempty"`
	DisableSSLValidation *bool                     `json:"disableSslValidation,omitempty"`
	EventParams          map[string]any            `json:"eventParams,omitempty"`
	Credential           *WebhookCredentialRequest `json:"credential,omitempty"`
	Enabled              *bool                     `json:"enabled,omitempty"`
	Template             json.RawMessage           `json:"template,omitempty"`
}
