package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
)

// StartChatOperation starts a chat and returns a handle that retains its latest
// state and emits Wait callbacks as the chat changes.
func (s *AI) StartChatOperation(
	ctx context.Context,
	networkID string,
	snapshotID string,
	prompt string,
) (*Poller[domain.AIChat], *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	chat, response, err := s.StartChat(ctx, networkID, snapshotID, prompt)
	if err != nil {
		return nil, response, err
	}
	poller, err := NewPoller(chat, func(ctx context.Context) (*domain.AIChat, *ports.Response, error) {
		return s.GetChat(ctx, string(chat.ID))
	}, func(value *domain.AIChat) (bool, error) {
		return strings.EqualFold(value.Status, domain.AIChatStatusDone), nil
	})
	return poller, response, err
}
