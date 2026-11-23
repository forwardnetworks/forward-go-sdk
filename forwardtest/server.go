// Package forwardtest is a fake Forward appserver for tests of code that uses the forward client. It answers like the real thing
// where consumers' own fakes tend not to: an unmapped route is Forward's own "No endpoint GET /api/..." 404 (which the client reads as
// ErrEndpointNotServed), and the error replies below carry the exact bodies the appserver writes, so a fake 429, a feature gate or a
// permission refusal classifies as the same domain.ErrorKind as the real one.
//
// Typical use:
//
//	srv := forwardtest.New(t)
//	srv.Handle(http.MethodGet, "/api/networks", forwardtest.JSON(http.StatusOK, []map[string]any{{"id": "1", "name": "lab"}}))
//	srv.Handle(http.MethodPost, "/api/networks/1/change-sets/c1/draft/devices/edge/bgp-advertisements", forwardtest.RateLimited(1), forwardtest.JSON(http.StatusOK, map[string]any{}))
//	client := srv.Client()
//
// The shapes here are the ones the forward package's own tests pin; where one was only read from Forward's source and never seen
// live, the doc comment says so. The package has no dependency beyond the standard library and the forward package.
package forwardtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

// Request is one request the fake received.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Body   []byte
}

// Reply is one canned answer. Build it with JSON or one of the error constructors.
type Reply struct {
	Status int
	Header http.Header
	Body   []byte
}

type route struct {
	method  string
	pattern []string // path segments; "*" matches any one segment
	query   url.Values
	replies []Reply
	handler http.HandlerFunc
	served  int
}

// Server is a fake Forward. The zero value is not usable; start one with New.
type Server struct {
	tb  testing.TB
	srv *httptest.Server

	mu       sync.Mutex
	routes   []*route
	requests []Request
	strict   bool
}

// New starts a fake Forward and stops it when the test ends.
func New(tb testing.TB) *Server {
	tb.Helper()
	s := &Server{tb: tb}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	tb.Cleanup(s.srv.Close)
	return s
}

// URL is the fake's base URL, for a forward.Config of your own.
func (s *Server) URL() string { return s.srv.URL }

// Strict makes a request no route handles fail the test (it still answers with the "No endpoint" 404). Off by default, because code that
// probes for a route on purpose, to fall back when it is missing, should see the same 404 the real appserver gives.
func (s *Server) Strict() *Server {
	s.mu.Lock()
	s.strict = true
	s.mu.Unlock()
	return s
}

// Client returns a forward client for the fake: user credentials, a transport of its own (so closing the server cannot disturb other
// tests), and no retries unless a config function sets Retry. Pass functions to change the Config.
func (s *Server) Client(configure ...func(*forward.Config)) *forward.Client {
	s.tb.Helper()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	s.tb.Cleanup(transport.CloseIdleConnections)
	cfg := forward.Config{
		BaseURL:    s.srv.URL,
		Username:   "user",
		Password:   "pass",
		HTTPClient: &http.Client{Transport: transport},
	}
	for _, fn := range configure {
		fn(&cfg)
	}
	client, err := forward.NewClient(cfg)
	if err != nil {
		s.tb.Fatalf("forwardtest: NewClient: %v", err)
	}
	return client
}

// Handle answers requests to method and path with the replies in order; the last reply repeats once the list runs out. The path is
// matched without its query string, and a "*" segment matches any one segment (/api/networks/*/snapshots). A later Handle for the
// same method and path replaces nothing: the most specific route wins (a query condition outranks any path; then the route with more
// literal segments), then the earliest registered.
func (s *Server) Handle(method, path string, replies ...Reply) {
	s.add(&route{method: method, pattern: segments(path), replies: replies})
}

// HandleQuery is Handle for requests whose query string carries every given parameter, such as the ?action= that Forward uses to
// tell several POSTs on one path apart. A route with query conditions wins over one without.
func (s *Server) HandleQuery(method, path string, query url.Values, replies ...Reply) {
	s.add(&route{method: method, pattern: segments(path), query: query, replies: replies})
}

// HandleFunc answers with a handler of your own, for a reply that depends on the request.
func (s *Server) HandleFunc(method, path string, handler http.HandlerFunc) {
	s.add(&route{method: method, pattern: segments(path), handler: handler})
}

func (s *Server) add(r *route) {
	if len(r.replies) == 0 && r.handler == nil {
		s.tb.Fatalf("forwardtest: a route for %s needs at least one reply", r.method)
	}
	s.mu.Lock()
	s.routes = append(s.routes, r)
	s.mu.Unlock()
}

// Requests returns every request received so far, oldest first.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// RequestsTo returns the requests to one method and path (a "*" segment matches any one segment).
func (s *Server) RequestsTo(method, path string) []Request {
	var out []Request
	for _, r := range s.Requests() {
		if r.Method == method && matchSegments(segments(path), segments(r.Path)) {
			out = append(out, r)
		}
	}
	return out
}

// Reset forgets the recorded requests; routes stay.
func (s *Server) Reset() {
	s.mu.Lock()
	s.requests = nil
	s.mu.Unlock()
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	s.mu.Lock()
	s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Body: body})
	var best *route
	bestScore := -1
	for _, candidate := range s.routes {
		if candidate.method != r.Method || !matchSegments(candidate.pattern, segments(r.URL.Path)) || !hasQuery(r.URL.Query(), candidate.query) {
			continue
		}
		// Specificity: every query condition outweighs any path (1000 each), then a literal path segment counts 1 and a "*" segment 0. A
		// route that fits is always eligible (the score is never negative), the highest score wins and the earliest registered wins a tie.
		score := len(candidate.query) * 1000
		for _, segment := range candidate.pattern {
			if segment != "*" {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = candidate, score
		}
	}
	var reply Reply
	if best != nil && best.handler == nil {
		index := best.served
		if index >= len(best.replies) {
			index = len(best.replies) - 1
		}
		reply = best.replies[index]
		best.served++
	}
	strict := s.strict
	s.mu.Unlock()

	switch {
	case best == nil:
		if strict {
			s.tb.Errorf("forwardtest: no route for %s %s", r.Method, r.URL.RequestURI())
		}
		EndpointNotServed(r.Method, r.URL.RequestURI()).write(w)
	case best.handler != nil:
		best.handler(w, r)
	default:
		reply.write(w)
	}
}

func (r Reply) write(w http.ResponseWriter) {
	for key, values := range r.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if w.Header().Get("Content-Type") == "" && len(r.Body) != 0 {
		w.Header().Set("Content-Type", "application/json")
	}
	status := r.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(r.Body)
}

func segments(path string) []string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	return strings.Split(strings.Trim(path, "/"), "/")
}

func matchSegments(pattern, path []string) bool {
	if len(pattern) != len(path) {
		return false
	}
	for i := range pattern {
		if pattern[i] != "*" && pattern[i] != path[i] {
			return false
		}
	}
	return true
}

func hasQuery(have, want url.Values) bool {
	for key, values := range want {
		got := have[key]
		for _, value := range values {
			found := false
			for _, g := range got {
				found = found || g == value
			}
			if !found {
				return false
			}
		}
	}
	return true
}

// JSON is a reply with a JSON body.
func JSON(status int, body any) Reply {
	raw, err := json.Marshal(body)
	if err != nil {
		panic(fmt.Sprintf("forwardtest: cannot marshal a reply body: %v", err))
	}
	return Reply{Status: status, Body: raw}
}

// NoContent is a 204.
func NoContent() Reply { return Reply{Status: http.StatusNoContent} }

func errorReply(status int, method, path, message string, extra map[string]any) Reply {
	body := map[string]any{"httpStatusCode": status, "apiUrl": path, "httpMethod": method, "message": message}
	maps.Copy(body, extra)
	return JSON(status, body)
}

// EndpointNotServed is what Forward answers for a route this deployment does not map: a 404 whose message is Spring's "No endpoint
// <METHOD> <URL>." The client reads it as domain.ErrEndpointNotServed, never as a missing network.
func EndpointNotServed(method, requestURI string) Reply {
	return errorReply(http.StatusNotFound, method, requestURI, "No endpoint "+method+" "+requestURI+".", nil)
}

// RateLimited is the per-user limiter's 429 with the remaining lockout in Retry-After (seconds). The client reads it as
// domain.ErrRateLimited.
func RateLimited(retryAfterSeconds int) Reply {
	reply := errorReply(http.StatusTooManyRequests, "", "", "Rate limit exceeded, please try again later", nil)
	reply.Header = http.Header{"Retry-After": []string{strconv.Itoa(retryAfterSeconds)}}
	return reply
}

// FeatureGated is the 403 for a route whose organization or deployment property is not set the way the route needs ("<PROPERTY> is
// off for your organization"). scope is "organization" or "deployment". The client reads it as domain.ErrFeatureGated.
func FeatureGated(property string, enabled bool, scope string) Reply {
	state := "off"
	if enabled {
		state = "on"
	}
	return errorReply(http.StatusForbidden, "", "", fmt.Sprintf("%s is %s for your %s", property, state, scope), nil)
}

// MissingPermission is the 403 AccessEnforcer writes for an operation the user lacks, e.g. MissingPermission("NetworkOperation",
// "EDIT_CHECKS") for "Missing permission: NetworkOperation.EDIT_CHECKS". The client reads it as domain.ErrPermissionDenied.
func MissingPermission(group, operation string) Reply {
	return errorReply(http.StatusForbidden, "", "", fmt.Sprintf("Missing permission: %s.%s", group, operation), nil)
}

// Unauthorized is a 401 for bad credentials; the client reads it as domain.ErrAuthentication.
func Unauthorized() Reply {
	return errorReply(http.StatusUnauthorized, "", "", "Bad credentials", nil)
}

// ActionNotSupported is Spring's 400 for an unmapped ?action= on a route, which an older build answers for a newer action. The client
// reads it as domain.ErrActionNotSupported.
func ActionNotSupported(action string) Reply {
	return errorReply(http.StatusBadRequest, "", "", fmt.Sprintf(`Parameter conditions "action=%s" not met for actual request parameters: action={other}`, action), nil)
}

// SnapshotNotProcessed is the 409 a snapshot-scoped route answers while the snapshot is still processing (reason SNAPSHOT_UNAVAILABLE).
// The client reads it as domain.ErrSnapshotNotProcessed.
func SnapshotNotProcessed() Reply {
	return errorReply(http.StatusConflict, "", "", "The snapshot is not available yet", map[string]any{"reason": "SNAPSHOT_UNAVAILABLE"})
}

// ServiceNowAsleep is the 409 Forward answers when the ServiceNow instance is hibernating (seen live on 26.10.6). The client reads it as
// domain.ErrServiceNowInstanceAsleep.
func ServiceNowAsleep() Reply {
	return errorReply(http.StatusConflict, "", "", "ServiceNow instance is hibernating", nil)
}

// BadRequest is a plain 400 with a message.
func BadRequest(message string) Reply { return errorReply(http.StatusBadRequest, "", "", message, nil) }

// ServerError is a 5xx with a message, for retry and timeout tests.
func ServerError(status int, message string) Reply { return errorReply(status, "", "", message, nil) }
