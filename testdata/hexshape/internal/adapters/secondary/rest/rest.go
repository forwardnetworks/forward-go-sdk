// Package rest is the fixture's HTTP adapter.
package rest

import (
	"context"
	"net/http"

	"example.com/hexshape/ports"
)

// Client sends.
type Client struct{ HTTP *http.Client }

func (c *Client) send(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://x"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	return err
}

// base is what every family type embeds; it answers ports' Network() for all of them.
type base struct{ network string }

// Network is the bound network.
func (b *base) Network() string { return b.network }

// Diffs implements ports.Diffs; Network comes from the embedded base.
type Diffs struct {
	*base
	C *Client
}

func (d *Diffs) Count(ctx context.Context, a, b string) (ports.Count, error) {
	return ports.Count{}, d.C.send(ctx, "/count")
}

func (d *Diffs) FilesCount(ctx context.Context, a, b string) (ports.Count, error) {
	return ports.Count{}, d.C.send(ctx, "/files")
}

func (d *Diffs) Summary(ctx context.Context) error { return d.C.send(ctx, "/summary") }

// Recount sends nothing itself: it calls Summary, which the use cases override.
func (d *Diffs) Recount(ctx context.Context) error { return d.Summary(ctx) }

func (d *Diffs) Kinds() []string { return []string{"devices"} }

// NewDiffs builds the Diffs family with its base.
func NewDiffs(c *Client) *Diffs { return &Diffs{base: &base{}, C: c} }

// LoggedDiffs wraps Diffs by embedding it: its methods are Diffs' own, so it is the same implementation, not a second one.
type LoggedDiffs struct{ *Diffs }

// Networks implements ports.Networks.
type Networks struct{ C *Client }

func (n *Networks) List(ctx context.Context) ([]string, error) {
	return nil, n.C.send(ctx, "/networks")
}

func (n *Networks) Bound() string { return "" }
