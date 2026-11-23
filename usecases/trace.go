package usecases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
)

// Explain runs Facets and then Trace for every path it lists, up to ExplainLimit.
// Forward groups paths by device path, so parallel links between the same two
// devices and different behaviours on one device path appear as ONE entry (only
// its sample behaviour is traced); alternatives through different devices each
// get their own. It returns ErrTraceAmbiguousSource when the source could be at
// several places. A path whose trace fails is reported on its own entry; an
// error that would fail every path (no permission, unknown network, route not
// served, rate limit) or a cancelled context fails the call.
func (s *Trace) Explain(ctx context.Context, networkID, snapshotID string, q domain.TraceQuery) (*domain.TraceExplanation, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	facets, resp, err := s.Facets(ctx, networkID, snapshotID, q, domain.TraceFacetsOptions{})
	if err != nil {
		return nil, resp, err
	}
	return s.explain(ctx, facets, resp, func(sel domain.TraceSelection) (*domain.TraceResult, *ports.Response, error) {
		return s.Trace(ctx, networkID, snapshotID, q, sel)
	})
}

// explain traces every path of a facets answer through traceOne, up to ExplainLimit. Explain and ExplainQuery share it.
func (s *Trace) explain(ctx context.Context, facets *domain.TraceFacets, resp *ports.Response, traceOne func(domain.TraceSelection) (*domain.TraceResult, *ports.Response, error)) (*domain.TraceExplanation, *ports.Response, error) {
	out := &domain.TraceExplanation{FacetsTimedOut: facets.TimedOut}
	if len(facets.TotalResults) > 0 {
		var total struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(facets.TotalResults, &total)
		out.ResultsLowerBound = total.Type == "LOWER_BOUND"
	}
	if len(facets.PathGroups) == 0 && len(facets.PossibleIPLocations) > 0 && string(facets.PossibleIPLocations) != "null" {
		return nil, resp, fmt.Errorf("%w: %s", domain.ErrTraceAmbiguousSource, facets.PossibleIPLocations)
	}
	for gi, g := range facets.PathGroups {
		for pi := range g.Paths {
			if len(out.Paths) >= domain.ExplainLimit {
				out.Truncated = true
				return out, resp, nil
			}
			sel, _ := g.Selection(pi)
			tp := domain.TracedPath{ID: fmt.Sprintf("g%d.p%d", gi, pi), Group: gi, Index: pi, TraceType: g.TraceType, Path: sel.Path}
			trace, _, terr := traceOne(sel)
			if terr != nil {
				if ctx.Err() != nil {
					return out, resp, ctx.Err()
				}
				if ports.IsStatus(terr, http.StatusUnauthorized) || ports.IsStatus(terr, http.StatusForbidden) || ports.IsStatus(terr, http.StatusNotFound) ||
					ports.IsStatus(terr, http.StatusTooManyRequests) || errors.Is(terr, domain.ErrEndpointNotServed) {
					return out, resp, terr
				}
				tp.Err, tp.Error = terr, terr.Error()
			} else {
				tp.Trace, tp.Descriptor = trace, trace.PathDescriptor
			}
			out.Paths = append(out.Paths, tp)
		}
	}
	return out, resp, nil
}
