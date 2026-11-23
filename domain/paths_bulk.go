package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
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
// limits are Forward's: MaxCandidates 1 to 10000 (default 5000), MaxResults 1 to 10000 (default 1), MaxReturnPathResults 0 to 10000
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
	// A result is parsed once unless it can be the error object: a bulk request carries up to 20000 results. The key is a plain
	// substring test, so a false positive (a name containing "error") only costs the probe parse.
	if !bytes.Contains(data, []byte(`"error"`)) {
		response := new(PathSearchResponse)
		if err := json.Unmarshal(data, response); err != nil {
			return err
		}
		*r = PathBulkResult{Response: response}
		return nil
	}
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

// WorstCase is how long Forward can take: the overall limit (its default is the per-query limit) plus one more per-query run for the
// queries already started when it expires, plus two minutes for resolution, queueing and the transfer.
func (o PathBulkOptions) WorstCase() time.Duration {
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

func (o PathBulkOptions) Validate() error {
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
