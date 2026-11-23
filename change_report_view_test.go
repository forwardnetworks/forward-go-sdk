package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The change-report reader tests read a report through what it publishes, its JSON, so they hold for a ChangeReport whose
// document is private to the domain package (the v0.9 hexagonal split). json.Number keeps the counts as integers for asInt.

func reportDoc(report *domain.ChangeReport) map[string]any {
	decoder := json.NewDecoder(bytes.NewReader(report.JSON()))
	decoder.UseNumber()
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		panic(fmt.Sprintf("a change report's JSON does not decode: %v", err))
	}
	return doc
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

func identityKey(identity any) []string {
	var out []string
	for _, v := range asList(identity) {
		out = append(out, asString(v))
	}
	return out
}
