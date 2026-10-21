package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MaxPathBulkQueries is the most queries Forward accepts in one bulk request. Forward recommends about 1,000 per request to maximize
// throughput.
const MaxPathBulkQueries = 20000

// PathQuery is one path search in a bulk request: the fields of PathSearchRequest that belong to a single question. Either From or
// SrcIP is required and DstIP always is. The settings that apply to the whole request (snapshot, intent, limits) are PathBulkOptions.
type PathQuery struct {
	From        string `json:"from,omitempty"`
	SrcIP       string `json:"srcIp,omitempty"`
	DstIP       string `json:"dstIp,omitempty"`
	IPProto     *int   `json:"ipProto,omitempty"`
	SrcPort     string `json:"srcPort,omitempty"`
	DstPort     string `json:"dstPort,omitempty"`
	IcmpType    *int   `json:"icmpType,omitempty"`
	FIN         *int   `json:"fin,omitempty"`
	SYN         *int   `json:"syn,omitempty"`
	RST         *int   `json:"rst,omitempty"`
	PSH         *int   `json:"psh,omitempty"`
	ACK         *int   `json:"ack,omitempty"`
	URG         *int   `json:"urg,omitempty"`
	AppID       string `json:"appId,omitempty"`
	UserID      string `json:"userId,omitempty"`
	UserGroupID string `json:"userGroupId,omitempty"`
	URL         string `json:"url,omitempty"`
	Domain      string `json:"domain,omitempty"`
}

// PathBulkOptions are the settings shared by every query of a bulk request; a nil or empty field is left to Forward's default. The
// limits are Forward's: MaxCandidates 1 to 10000 (default 5000), MaxResults at least 1 (default 1), MaxReturnPathResults 0 to 10000
// (default 0: no return paths), MaxSeconds 1 to 300 per query (default 30), MaxOverallSeconds 1 to 7200 for the whole request.
// Forward also requires MaxResults not to exceed the effective MaxCandidates and MaxSeconds not to exceed the effective MaxOverallSeconds,
// and answers 400 otherwise.
type PathBulkOptions struct {
	SnapshotID string
	// Intent is PREFER_DELIVERED (Forward's default), PREFER_VIOLATIONS or VIOLATIONS_ONLY.
	Intent               string
	MaxCandidates        *int
	MaxResults           *int
	MaxReturnPathResults *int
	MaxSeconds           *int
	// MaxOverallSeconds bounds the whole request; MaxSeconds must not exceed it. Forward's default is the per-query timeout, so a large
	// bulk request needs this set: when it passes, the queries that have not STARTED return a result with TimedOut true and no paths
	// (not a failure, so check TimedOut as well as Failed), while those already running finish under MaxSeconds. Above 10 queries Forward
	// runs the request as a large job with a one hour limit, so a value above 3600 may be cut short. Forward writes nothing until every
	// query is done, so this SDK lengthens a shorter client timeout to MaxOverallSeconds plus MaxSeconds plus two minutes.
	MaxOverallSeconds       *int
	IncludeTags             *bool
	IncludeNetworkFunctions *bool
}

// PathBulkResult is the outcome of one query. Forward evaluates the queries independently: one that failed (for example a device that
// is not in the snapshot) comes back as an error object in its place and does not fail the request. Check Failed before using Response.
type PathBulkResult struct {
	// Response is the path search result, nil when Failed.
	Response *PathSearchResponse
	// Failed says Forward answered an error object for this query; ErrorMessage is its text.
	Failed       bool
	ErrorMessage string
}

// UnmarshalJSON reads either a PathSearchResponse or Forward's {"error": true, "errorMessage": "..."} object.
func (r *PathBulkResult) UnmarshalJSON(data []byte) error {
	var probe struct {
		Error        bool   `json:"error"`
		ErrorMessage string `json:"errorMessage"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Error {
		*r = PathBulkResult{Failed: true, ErrorMessage: probe.ErrorMessage}
		return nil
	}
	response := new(PathSearchResponse)
	if err := json.Unmarshal(data, response); err != nil {
		return err
	}
	*r = PathBulkResult{Response: response}
	return nil
}

func validatePathQuery(i int, q PathQuery) error {
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

// worstCase is how long Forward can take: the overall limit (its default is the per-query limit) plus one more per-query run for the
// queries already started when it expires, plus two minutes for resolution, queueing and the transfer.
func (o PathBulkOptions) worstCase() time.Duration {
	perQuery := 30
	if o.MaxSeconds != nil {
		perQuery = *o.MaxSeconds
	}
	overall := perQuery
	if o.MaxOverallSeconds != nil {
		overall = *o.MaxOverallSeconds
	}
	return time.Duration(overall+perQuery)*time.Second + 2*time.Minute
}

func (o PathBulkOptions) validate() error {
	switch o.Intent {
	case "", "PREFER_DELIVERED", "PREFER_VIOLATIONS", "VIOLATIONS_ONLY":
	default:
		return fmt.Errorf("forward: intent %q must be PREFER_DELIVERED, PREFER_VIOLATIONS or VIOLATIONS_ONLY", o.Intent)
	}
	check := func(name string, value *int, min, max int) error {
		if value != nil && (*value < min || *value > max) {
			return fmt.Errorf("forward: %s must be between %d and %d", name, min, max)
		}
		return nil
	}
	for _, c := range []error{
		check("maxCandidates", o.MaxCandidates, 1, 10000),
		check("maxResults", o.MaxResults, 1, 10000),
		check("maxReturnPathResults", o.MaxReturnPathResults, 0, 10000),
		check("maxSeconds", o.MaxSeconds, 1, 300),
		check("maxOverallSeconds", o.MaxOverallSeconds, 1, 7200),
	} {
		if c != nil {
			return c
		}
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
func (s *NetworksService) PathsBulk(ctx context.Context, networkID string, queries []PathQuery, options PathBulkOptions) ([]PathBulkResult, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	if len(queries) == 0 || len(queries) > MaxPathBulkQueries {
		return nil, nil, fmt.Errorf("forward: a bulk path search takes 1 to %d queries, not %d", MaxPathBulkQueries, len(queries))
	}
	for i, q := range queries {
		if err := validatePathQuery(i, q); err != nil {
			return nil, nil, err
		}
	}
	if err := options.validate(); err != nil {
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
		if need := options.worstCase(); hc.Timeout < need {
			timeout = need
		}
	}
	var results []PathBulkResult
	resp, err := s.client.doWithTimeout(req, &results, false, nil, timeout)
	if err != nil {
		return nil, resp, err
	}
	if len(results) != len(queries) {
		return nil, resp, errors.New("forward: bulk path search answered a different number of results than queries")
	}
	return results, resp, nil
}
