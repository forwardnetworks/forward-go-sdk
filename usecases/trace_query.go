package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// ExplainQuery is Explain for a TracePathQuery: FacetsQuery and then TraceFor for every path it lists, up to ExplainLimit, with the same
// rules as Explain for what fails one path and what fails the call. The paths of a query with a Chain or a To location, which Trace and
// Explain cannot express, are listed this way. Source-only like FacetsQuery.
func (s *Trace) ExplainQuery(ctx context.Context, networkID, snapshotID string, q domain.TracePathQuery) (*domain.TraceExplanation, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	facets, resp, err := s.FacetsQuery(ctx, networkID, snapshotID, q, domain.TraceFacetsOptions{})
	if err != nil {
		return nil, resp, err
	}
	return s.explain(ctx, facets, resp, func(sel domain.TraceSelection) (*domain.TraceResult, *ports.Response, error) {
		return s.TraceFor(ctx, networkID, snapshotID, q, sel)
	})
}
