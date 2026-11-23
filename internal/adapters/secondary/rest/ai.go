package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AIService accesses Forward's snapshot-grounded AI chat APIs.
//
// Preview: these endpoints are not part of the published OpenAPI contract and
// are gated by the AI_ALLOWED and AI_CHATS organization properties.
type AI service

// ListChats lists the authenticated user's AI chats.
func (s *AI) ListChats(ctx context.Context) ([]ports.AIChat, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/ai-chats", nil)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.AIChat]{Keys: []string{"chats", "items"}}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
}

// StartChat starts an asynchronous AI chat grounded in a processed snapshot.
func (s *AI) StartChat(
	ctx context.Context,
	networkID string,
	snapshotID string,
	prompt string,
) (*ports.AIChat, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	chat := new(ports.AIChat)
	resp, err := s.client.Do(req, chat)
	if err != nil {
		return nil, resp, err
	}
	return chat, resp, nil
}

// GetChat returns AI chat metadata, including its processing status.
func (s *AI) GetChat(ctx context.Context, chatID string) (*ports.AIChat, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := aiChatPath(chatID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	chat := new(ports.AIChat)
	resp, err := s.client.Do(req, chat)
	if err != nil {
		return nil, resp, err
	}
	return chat, resp, nil
}

// RenameChat updates an AI chat's display name.
func (s *AI) RenameChat(ctx context.Context, chatID, name string) (*ports.AIChat, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	chat := new(ports.AIChat)
	resp, err := s.client.Do(req, chat)
	if err != nil {
		return nil, resp, err
	}
	return chat, resp, nil
}

// DeleteChat deletes an AI chat.
func (s *AI) DeleteChat(ctx context.Context, chatID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *AI) AddMessage(ctx context.Context, networkID, chatID, prompt string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *AI) ListMessages(
	ctx context.Context,
	chatID string,
	options ports.AIMessageListOptions,
) ([]ports.AIMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	result := listResponse[ports.AIMessage]{Keys: []string{"messages", "items"}}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
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
