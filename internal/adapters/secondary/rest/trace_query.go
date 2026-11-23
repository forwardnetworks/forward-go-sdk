package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// A typed path query for Trace.FacetsQuery: the GUI Search page's PathQuery (core/.../query/json/PathQuery.java at origin/master) with
// locations, through hops, header filters and the PERMIT_ALL mode, where TraceQuery covers one IPv4 flow. PREVIEW, like the rest of
// TraceService. Each shape below is marked "source-only" (read from Forward source) or "confirmed live" (seen by forward-skills on a primary-pin org,
// Forward 26.11.0-03, snapshot of a two-device NX-OS lab, read-only probes; the lab had no IPv6, no internet node and no chain hop in the probes);
// anything not marked confirmed was not run.

// FacetsQuery is Facets for a TracePathQuery: the distinct paths the GUI Search page's query takes (POST /api/networks/{id}/trace-facets).
// An empty snapshotID uses the network's latest. See TraceFacets for the response, and Total and PossibleLocations for the typed readings
// of totalResults and possibleIpLocations. Read-only, sent as a POST, so a retry policy resends it only after a 429 or 503 or a failure to
// connect. A TraceQuery-based Facets is unchanged.
func (s *Trace) FacetsQuery(ctx context.Context, networkID, snapshotID string, q ports.TracePathQuery, opts ports.TraceFacetsOptions) (*ports.TraceFacets, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	filters, err := q.Filters()
	if err != nil {
		return nil, nil, err
	}
	return s.facets(ctx, networkID, snapshotID, filters, opts, "Trace.FacetsQuery")
}

// TraceFor is Trace for a TracePathQuery: the per-hop detail of ONE path chosen from FacetsQuery's PathGroups, with the same query, so a
// query with a Chain (through, ingress or egress hops), a To location or headers, or the PERMIT_ALL mode can be traced as well as counted
// (POST /api/networks/{id}/trace, the body the GUI Search page sends). An empty snapshotID uses the network's latest. Select the path with
// TracePathGroup.Selection. Source-only for every shape FacetsQuery marks source-only; the /trace route itself is the one Trace uses.
func (s *Trace) TraceFor(ctx context.Context, networkID, snapshotID string, q ports.TracePathQuery, sel ports.TraceSelection) (*ports.TraceResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	filters, err := q.Filters()
	if err != nil {
		return nil, nil, err
	}
	return s.trace(ctx, networkID, snapshotID, filters, sel, "Trace.TraceFor")
}
