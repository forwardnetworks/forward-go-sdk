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

// Networks implements ports.Networks.
var _ ports.Networks = (*Networks)(nil)

// Networks implements ports.Networks, again for the doc.
type Networks struct{ C *Client }

func (n *Networks) List(ctx context.Context) ([]string, error) {
	return nil, n.C.send(ctx, "/networks")
}

func (n *Networks) Bound() string { return "" }
