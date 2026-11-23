// Package usecases is the fixture's composites.
package usecases

import (
	"context"

	"example.com/hexshape/ports"
)

// Diffs adds composites to the port.
type Diffs struct{ ports.Diffs }

// MaterialSummary only calls the port.
func (d *Diffs) MaterialSummary(ctx context.Context, a, b string) (int64, error) {
	c, err := d.Count(ctx, a, b)
	if err != nil {
		return 0, err
	}
	f, err := d.FilesCount(ctx, a, b)
	return c.N + f.N, err
}

// WaitCount hands a method value of the port to a generic helper, as a poller does.
func (d *Diffs) WaitCount(ctx context.Context, a, b string) (ports.Count, error) {
	return poll(ctx, a, b, d.Count)
}

func poll[T any](ctx context.Context, a, b string, read func(context.Context, string, string) (T, error)) (T, error) {
	return read(ctx, a, b)
}

// Summary overrides the port's Summary and calls through to it.
func (d *Diffs) Summary(ctx context.Context) error { return d.Diffs.Summary(ctx) }

// Networks has no composites.
type Networks struct{ ports.Networks }

// Reports is a cross-family composite: it is given other families' ports, embedding none.
type Reports struct {
	diffs    ports.Diffs
	networks ports.Networks
}

// NewReports wires the ports it is given.
func NewReports(d ports.Diffs, n ports.Networks) *Reports { return &Reports{diffs: d, networks: n} }

// Compare reads both families.
func (r *Reports) Compare(ctx context.Context) error {
	if _, err := r.diffs.Count(ctx, "a", "b"); err != nil {
		return err
	}
	_, err := r.networks.List(ctx)
	return err
}
