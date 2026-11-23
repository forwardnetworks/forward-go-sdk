package domain

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

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
	ID        Identifier        `json:"id"`
	Prompt    string            `json:"prompt"`
	ToolCalls []AIToolCall      `json:"toolCalls"`
	Tasks     []json.RawMessage `json:"tasks,omitempty"`
	// Answer is the current appserver shape (cv/ai/MessageAnswer, 26.9):
	// summary, outOfScope, keyInsights. Nil while the message is still being
	// processed.
	Answer *AIMessageAnswer `json:"answer,omitempty"`
	// OutOfScopeReason and FinalAnswer are the pre-26.9 shape, kept so an
	// older appserver still yields text. Use Answer()/OutOfScope() below
	// rather than reading either field.
	OutOfScopeReason string         `json:"outOfScopeReason,omitempty"`
	FinalAnswer      *AIFinalAnswer `json:"finalAnswer,omitempty"`
	CreatedAt        string         `json:"createdAt"`
	UpdatedAt        string         `json:"updatedAt"`
}

// AIMessageAnswer is the answer produced at the end of the tool loop.
type AIMessageAnswer struct {
	Summary     string   `json:"summary"`
	OutOfScope  bool     `json:"outOfScope"`
	KeyInsights []string `json:"keyInsights,omitempty"`
}

// AnswerText returns the human answer from whichever shape the appserver sent
// (summary plus key insights, or the legacy finalAnswer), "" when the message
// has no answer yet or is out of scope.
func (m *AIMessage) AnswerText() string {
	if m == nil {
		return ""
	}
	if m.Answer != nil {
		if m.Answer.OutOfScope {
			return ""
		}
		var b strings.Builder
		b.WriteString(strings.TrimSpace(m.Answer.Summary))
		if len(m.Answer.KeyInsights) > 0 {
			b.WriteString("\n\nKey insights:")
			for _, k := range m.Answer.KeyInsights {
				if k = strings.TrimSpace(k); k != "" {
					b.WriteString("\n- " + k)
				}
			}
		}
		return strings.TrimSpace(b.String())
	}
	if strings.TrimSpace(m.OutOfScopeReason) != "" {
		return ""
	}
	return m.FinalAnswer.AnswerText()
}

// OutOfScope returns the reason Forward declined the prompt, "" otherwise.
func (m *AIMessage) OutOfScope() string {
	if m == nil {
		return ""
	}
	if m.Answer != nil && m.Answer.OutOfScope {
		return strings.TrimSpace(m.Answer.Summary)
	}
	return strings.TrimSpace(m.OutOfScopeReason)
}

// AIMessageListOptions filters messages updated after Since.
type AIMessageListOptions struct {
	Since *time.Time
}

func rawString(raw json.RawMessage) string {
	var value string
	if len(raw) != 0 && json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	return ""
}
