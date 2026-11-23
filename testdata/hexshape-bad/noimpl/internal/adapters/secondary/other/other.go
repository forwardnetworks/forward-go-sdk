// Package other implements ports.Diffs outside the wired adapter package.
package other

import (
	"context"

	"example.com/hexshape/ports"
)

// Diffs implements ports.Diffs.
type Diffs struct{}

func (d *Diffs) Count(ctx context.Context, a, b string) (ports.Count, error) {
	return ports.Count{}, nil
}

func (d *Diffs) FilesCount(ctx context.Context, a, b string) (ports.Count, error) {
	return ports.Count{}, nil
}

func (d *Diffs) Summary(ctx context.Context) error { return nil }

func (d *Diffs) Kinds() []string { return []string{"devices"} }
