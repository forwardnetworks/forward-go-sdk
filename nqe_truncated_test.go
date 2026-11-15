package forward

import (
	"encoding/json"
	"testing"
)

// A page with fewer rows than totalNumItems is a partial answer; a bare array carries no total and must not claim truncation.
func TestNQEResultTruncated(t *testing.T) {
	var page, bare NQEResult
	if err := json.Unmarshal([]byte(`{"items":[{"a":1}],"totalNumItems":3}`), &page); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`[{"a":1}]`), &bare); err != nil {
		t.Fatal(err)
	}
	if !page.Truncated() || bare.Truncated() || (*NQEResult)(nil).Truncated() {
		t.Fatalf("page=%v bare=%v", page.Truncated(), bare.Truncated())
	}
}
