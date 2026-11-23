// Package hexshape is the fixture's composition root.
package hexshape

import (
	"net/http"

	"example.com/hexshape/internal/adapters/secondary/other"
	"example.com/hexshape/internal/adapters/secondary/rest"
	"example.com/hexshape/usecases"
)

// Client is the fixture's client.
type Client struct {
	Diffs    *usecases.Diffs
	Networks *usecases.Networks
	Reports  *usecases.Reports
}

// New wires rest into the use cases.
func New() *Client {
	c := &rest.Client{HTTP: http.DefaultClient}
	d, n := &other.Diffs{}, &rest.Networks{C: c}
	return &Client{Diffs: &usecases.Diffs{Diffs: d}, Networks: &usecases.Networks{Networks: n}, Reports: usecases.NewReports(d, n)}
}
