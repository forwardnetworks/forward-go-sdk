// Package legacy is an unwired second implementation of ports.Diffs whose Kinds sends: the walk must not count it.
package legacy

import (
	"context"
	"net/http"

	"example.com/hexshape/ports"
)

// Diffs implements ports.Diffs, and is never wired.
type Diffs struct{}

func (Diffs) Count(ctx context.Context, a, b string) (ports.Count, error) { return ports.Count{}, nil }
func (Diffs) FilesCount(ctx context.Context, a, b string) (ports.Count, error) {
	return ports.Count{}, nil
}
func (Diffs) Summary(ctx context.Context) error { return nil }

// Network answers the port.
func (Diffs) Network() string { return "" }

func (Diffs) Kinds() []string {
	req, _ := http.NewRequest(http.MethodGet, "http://x/kinds", nil)
	_ = req
	return nil
}
