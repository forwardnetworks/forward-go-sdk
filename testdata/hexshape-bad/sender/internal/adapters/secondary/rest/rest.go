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

// Diffs implements ports.Diffs.
type Diffs struct{ C *Client }

func (d *Diffs) Count(ctx context.Context, a, b string) (ports.Count, error) {
	return ports.Count{}, d.C.send(ctx, "/count")
}

func (d *Diffs) FilesCount(ctx context.Context, a, b string) (ports.Count, error) {
	return ports.Count{}, d.C.send(ctx, "/files")
}

func (d *Diffs) Summary(ctx context.Context) error { return d.C.send(ctx, "/summary") }

func (d *Diffs) Kinds() []string { return []string{"devices"} }

// Networks implements ports.Networks.
type Networks struct{ C *Client }

func (n *Networks) List(ctx context.Context) ([]string, error) {
	return nil, n.C.send(ctx, "/networks")
}

func (n *Networks) Bound() string {
	if resp, err := http.Get("http://x/bound"); err == nil {
		resp.Body.Close()
	}
	return ""
}
