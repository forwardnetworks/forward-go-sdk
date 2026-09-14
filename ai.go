package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// AIService accesses Forward's snapshot-grounded AI chat APIs.
//
// Preview: these endpoints are not part of the published OpenAPI contract and
// are gated by the AI_ALLOWED and AI_CHATS organization properties.
type AIService service

const (
	AIChatStatusProcessing = "PROCESSING"
	AIChatStatusDone       = "DONE"
)

// AIChat is a snapshot-grounded Forward AI conversation.
type AIChat struct {
	ID         Identifier `json:"id"`
	NetworkID  Identifier `json:"networkId"`
	SnapshotID Identifier `json:"snapshotId"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	CreatedAt  string     `json:"createdAt"`
	UpdatedAt  string     `json:"updatedAt"`
}

// AIFinalAnswer is the structured answer produced after the tool loop.
type AIFinalAnswer struct {
	Summary     string   `json:"summary"`
	KeyInsights []string `json:"keyInsights,omitempty"`
	Text        string   `json:"-"`
	// Raw carries fields this SDK version does not model, so an object read
	// from a newer appserver and written back does not silently lose them.
	Raw map[string]json.RawMessage `json:"-"`
}

func (a *AIFinalAnswer) UnmarshalJSON(data []byte) error {
	var text string
	if json.Unmarshal(data, &text) == nil {
		a.Text = strings.TrimSpace(text)
		return nil
	}
	type plain AIFinalAnswer
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*a = AIFinalAnswer(value)
	a.Raw = raw
	return nil
}

// AnswerText extracts human-readable text while tolerating field-name changes
// between appserver builds.
func (a *AIFinalAnswer) AnswerText() string {
	if a == nil {
		return ""
	}
	if text := strings.TrimSpace(a.Text); text != "" {
		return text
	}
	for _, key := range []string{"answer", "text", "content", "markdown", "response", "summary"} {
		if text := rawString(a.Raw[key]); text != "" {
			return text
		}
	}
	keys := make([]string, 0, len(a.Raw))
	for key := range a.Raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		if text := rawString(a.Raw[key]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// AIToolCall describes one tool invoked by the Forward AI agent. Params and
// Result remain raw because their schemas depend on Type.
type AIToolCall struct {
	Type        string          `json:"type"`
	LLMID       Identifier      `json:"llmId,omitempty"`
	Params      json.RawMessage `json:"params,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	StartedAt   string          `json:"startedAt"`
	CompletedAt string          `json:"completedAt,omitempty"`
}

// AIMessage is one user prompt and the agent work performed for it.
type AIMessage struct {
	ID               Identifier        `json:"id"`
	Prompt           string            `json:"prompt"`
	OutOfScopeReason string            `json:"outOfScopeReason,omitempty"`
	ToolCalls        []AIToolCall      `json:"toolCalls"`
	Tasks            []json.RawMessage `json:"tasks,omitempty"`
	FinalAnswer      *AIFinalAnswer    `json:"finalAnswer,omitempty"`
	CreatedAt        string            `json:"createdAt"`
	UpdatedAt        string            `json:"updatedAt"`
}

// AIMessageListOptions filters messages updated after Since.
type AIMessageListOptions struct {
	Since *time.Time
}

// ListChats lists the authenticated user's AI chats.
func (s *AIService) ListChats(ctx context.Context) ([]AIChat, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/ai-chats", nil)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[AIChat]{Keys: []string{"chats", "items"}}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
}

// StartChat starts an asynchronous AI chat grounded in a processed snapshot.
func (s *AIService) StartChat(
	ctx context.Context,
	networkID string,
	snapshotID string,
	prompt string,
) (*AIChat, *Response, error) {
	var err error
	if networkID, err = s.client.resolveNetworkID(networkID); err != nil {
		return nil, nil, err
	}
	if err := validateAIChatInput(networkID, snapshotID, prompt); err != nil {
		return nil, nil, err
	}
	params := url.Values{
		"networkId":  []string{strings.TrimSpace(networkID)},
		"snapshotId": []string{strings.TrimSpace(snapshotID)},
	}
	req, err := s.client.newJSONRequest(
		ctx,
		http.MethodPost,
		"/api/ai-chats?"+params.Encode(),
		map[string]string{"prompt": strings.TrimSpace(prompt)},
	)
	if err != nil {
		return nil, nil, err
	}
	chat := new(AIChat)
	resp, err := s.client.Do(req, chat)
	if err != nil {
		return nil, resp, err
	}
	return chat, resp, nil
}

// StartChatOperation starts a chat and returns a handle that retains its latest
// state and emits Wait callbacks as the chat changes.
func (s *AIService) StartChatOperation(
	ctx context.Context,
	networkID string,
	snapshotID string,
	prompt string,
) (*Poller[AIChat], *Response, error) {
	chat, response, err := s.StartChat(ctx, networkID, snapshotID, prompt)
	if err != nil {
		return nil, response, err
	}
	poller, err := NewPoller(chat, func(ctx context.Context) (*AIChat, *Response, error) {
		return s.GetChat(ctx, string(chat.ID))
	}, func(value *AIChat) (bool, error) {
		return strings.EqualFold(value.Status, AIChatStatusDone), nil
	})
	return poller, response, err
}

// GetChat returns AI chat metadata, including its processing status.
func (s *AIService) GetChat(ctx context.Context, chatID string) (*AIChat, *Response, error) {
	path, err := aiChatPath(chatID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	chat := new(AIChat)
	resp, err := s.client.Do(req, chat)
	if err != nil {
		return nil, resp, err
	}
	return chat, resp, nil
}

// RenameChat updates an AI chat's display name.
func (s *AIService) RenameChat(ctx context.Context, chatID, name string) (*AIChat, *Response, error) {
	path, err := aiChatPath(chatID)
	if err != nil {
		return nil, nil, err
	}
	if name = strings.TrimSpace(name); name == "" {
		return nil, nil, errors.New("forward: AI chat name is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, map[string]string{"name": name})
	if err != nil {
		return nil, nil, err
	}
	chat := new(AIChat)
	resp, err := s.client.Do(req, chat)
	if err != nil {
		return nil, resp, err
	}
	return chat, resp, nil
}

// DeleteChat deletes an AI chat.
func (s *AIService) DeleteChat(ctx context.Context, chatID string) (*Response, error) {
	path, err := aiChatPath(chatID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// AddMessage appends a prompt to an AI chat. Processing is asynchronous; poll
// GetChat and then call ListMessages for the completed answer.
func (s *AIService) AddMessage(ctx context.Context, networkID, chatID, prompt string) (*Response, error) {
	path, err := aiChatPath(chatID)
	if err != nil {
		return nil, err
	}
	networkID, err = s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	if prompt = strings.TrimSpace(prompt); prompt == "" {
		return nil, errors.New("forward: AI prompt is required")
	}
	path += "/messages?" + url.Values{"networkId": []string{networkID}}.Encode()
	req, err := s.client.newJSONRequest(
		ctx,
		http.MethodPost,
		path,
		map[string]string{"prompt": prompt},
	)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// ListMessages returns messages in an AI chat.
func (s *AIService) ListMessages(
	ctx context.Context,
	chatID string,
	options AIMessageListOptions,
) ([]AIMessage, *Response, error) {
	path, err := aiChatPath(chatID)
	if err != nil {
		return nil, nil, err
	}
	path += "/messages"
	if options.Since != nil {
		path += "?" + url.Values{"since": []string{options.Since.Format(time.RFC3339Nano)}}.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[AIMessage]{Keys: []string{"messages", "items"}}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
}

func rawString(raw json.RawMessage) string {
	var value string
	if len(raw) != 0 && json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	return ""
}

func aiChatPath(chatID string) (string, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return "", errors.New("forward: AI chat ID is required")
	}
	return fmt.Sprintf("/api/ai-chats/%s", url.PathEscape(chatID)), nil
}

func validateAIChatInput(networkID, snapshotID, prompt string) error {
	if strings.TrimSpace(networkID) == "" || strings.TrimSpace(snapshotID) == "" {
		return errors.New("forward: network ID and snapshot ID are required")
	}
	if strings.TrimSpace(prompt) == "" {
		return errors.New("forward: AI prompt is required")
	}
	return nil
}
