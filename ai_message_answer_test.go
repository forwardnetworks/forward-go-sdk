package forward

import (
	"encoding/json"
	"testing"
)

// The appserver moved the AI message's answer from finalAnswer/outOfScopeReason
// to answer{summary,outOfScope,keyInsights} (cv/ai/MessageAnswer, 26.9). Both
// shapes must yield text; the legacy-only reader saw every DONE chat as empty.
func TestAIMessageAnswerTextReadsBothShapes(t *testing.T) {
	var cur AIMessage
	if err := json.Unmarshal([]byte(`{"id":0,"prompt":"q","toolCalls":[],"answer":{"summary":"Nine devices.","outOfScope":false,"keyInsights":["OSPF up"]}}`), &cur); err != nil {
		t.Fatal(err)
	}
	if got := cur.AnswerText(); got != "Nine devices.\n\nKey insights:\n- OSPF up" {
		t.Fatalf("current shape: %q", got)
	}
	if cur.OutOfScope() != "" {
		t.Fatal("in-scope answer must not report out of scope")
	}
	var oos AIMessage
	_ = json.Unmarshal([]byte(`{"answer":{"summary":"Ask about the network.","outOfScope":true}}`), &oos)
	if oos.AnswerText() != "" || oos.OutOfScope() != "Ask about the network." {
		t.Fatalf("out of scope: text=%q reason=%q", oos.AnswerText(), oos.OutOfScope())
	}
	var legacy AIMessage
	_ = json.Unmarshal([]byte(`{"finalAnswer":{"answer":"legacy text"}}`), &legacy)
	if legacy.AnswerText() != "legacy text" {
		t.Fatalf("legacy shape: %q", legacy.AnswerText())
	}
	var pending AIMessage
	_ = json.Unmarshal([]byte(`{"prompt":"q","toolCalls":[]}`), &pending)
	if pending.AnswerText() != "" || pending.OutOfScope() != "" {
		t.Fatal("a message still processing has no answer")
	}
}
