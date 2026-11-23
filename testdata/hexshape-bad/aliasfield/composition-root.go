// Package hexshape is the fixture's composition root.
package hexshape

import (
	"net/http"

	"example.com/hexshape/internal/adapters/secondary/rest"
	"example.com/hexshape/usecases"
)

// DiffsService is the Diffs family, by its old name.
type DiffsService = usecases.Diffs

// Client is the fixture's client.
type Client struct {
	Diffs    *DiffsService
	Networks *usecases.Networks
	Reports  *usecases.Reports
}

// New wires rest into the use cases.
func New() *Client {
	c := &rest.Client{HTTP: http.DefaultClient}
	d, n := &rest.Diffs{C: c}, &rest.Networks{C: c}
	return &Client{Diffs: &usecases.Diffs{Diffs: d}, Networks: &usecases.Networks{Networks: n}, Reports: usecases.NewReports(d, n)}
}
