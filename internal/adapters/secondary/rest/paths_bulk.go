package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func validatePathQuery(i int, q ports.PathQuery) error {
	if strings.TrimSpace(q.DstIP) == "" {
		return fmt.Errorf("forward: query %d: destination IP is required", i)
	}
	if strings.TrimSpace(q.From) == "" && strings.TrimSpace(q.SrcIP) == "" {
		return fmt.Errorf("forward: query %d: either from or srcIp is required", i)
	}
	if q.URL != "" && q.Domain != "" {
		return fmt.Errorf("forward: query %d: url and domain cannot be combined", i)
	}
	return nil
}

// PathsBulk runs many path searches against one snapshot in a single request (POST /api/networks/{networkId}/paths-bulk, published,
// VIEW_PATHS; the snapshot must be processed to the reachability stage) and returns one result per query, in the order of queries.
//
// Between 1 and MaxPathBulkQueries (20000) queries are accepted; Forward recommends about 1000 per request and answers 400 beyond the
// limit, which this SDK refuses first. A malformed query (a bad address, ports with a protocol other than 6 or 17, an ICMP type with a
// port) fails the WHOLE request with a 400 naming it; only a problem found while searching (a device not in the snapshot, a compute
// error) becomes a per-query failure. Each result is a path search response like Networks.Paths returns, with ReturnPathInfo filled in
// when MaxReturnPathResults is above zero (always empty for a multicast query), or a failure for that query alone (see PathBulkResult).
// A matrix of A to B and B to A searches is two queries per pair. The whole body is read before PathsBulk returns; Forward's
// streaming variant (paths-bulk-seq) is not wrapped.
//
// This is a read sent as a POST, so a retry policy resends it only after a 429 or 503 or a failure to connect; wrap the context in
// WithIdempotent to also resend it after a gateway error or a lost connection, bearing in mind that Forward is probably still
// computing the first attempt.
func (s *Networks) PathsBulk(ctx context.Context, networkID string, queries []ports.PathQuery, options ports.PathBulkOptions) ([]ports.PathBulkResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	if len(queries) == 0 || len(queries) > ports.MaxPathBulkQueries {
		return nil, nil, fmt.Errorf("forward: a bulk path search takes 1 to %d queries, not %d", ports.MaxPathBulkQueries, len(queries))
	}
	for i, q := range queries {
		if err := validatePathQuery(i, q); err != nil {
			return nil, nil, err
		}
	}
	if err := options.Validate(); err != nil {
		return nil, nil, err
	}
	body := map[string]any{"queries": queries}
	if options.Intent != "" {
		body["intent"] = options.Intent
	}
	for name, value := range map[string]*int{
		"maxCandidates": options.MaxCandidates, "maxResults": options.MaxResults, "maxReturnPathResults": options.MaxReturnPathResults,
		"maxSeconds": options.MaxSeconds, "maxOverallSeconds": options.MaxOverallSeconds,
	} {
		if value != nil {
			body[name] = *value
		}
	}
	if options.IncludeTags != nil {
		body["includeTags"] = *options.IncludeTags
	}
	if options.IncludeNetworkFunctions != nil {
		body["includeNetworkFunctions"] = *options.IncludeNetworkFunctions
	}
	path := "/api/networks/" + url.PathEscape(networkID) + "/paths-bulk"
	if snapshotID := strings.TrimSpace(options.SnapshotID); snapshotID != "" {
		path += "?" + url.Values{"snapshotId": []string{snapshotID}}.Encode()
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.PathsBulk")
	// Forward checks the overall deadline only before it starts each query and writes nothing until every query is done, so the
	// answer can take the overall time plus one more full per-query run. Only ever lengthen the client's timeout: an unlimited or
	// longer one stays.
	var timeout time.Duration
	if hc := s.client.httpClient; hc != nil && hc.Timeout > 0 {
		if need := options.WorstCase(); hc.Timeout < need {
			timeout = need
		}
	}
	var results []ports.PathBulkResult
	resp, err := s.client.doWithTimeout(req, &results, false, nil, timeout)
	if err != nil {
		return nil, resp, err
	}
	if len(results) != len(queries) {
		return nil, resp, errors.New("forward: bulk path search answered a different number of results than queries")
	}
	return results, resp, nil
}
