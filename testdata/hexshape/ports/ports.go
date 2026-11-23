// Package ports is the fixture's contracts.
package ports

import (
	"context"

	"example.com/hexshape/domain"
)

// Count is re-exported for the adapter.
type Count = domain.Count

// Diffs reads diff counts.
type Diffs interface {
	Count(ctx context.Context, a, b string) (Count, error)
	FilesCount(ctx context.Context, a, b string) (Count, error)
	Summary(ctx context.Context) error
	Recount(ctx context.Context) error
	Kinds() []string
	Network() string
}

// Networks lists networks.
type Networks interface {
	List(ctx context.Context) ([]string, error)
	Bound() string
}
