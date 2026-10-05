package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Spring answers a path no controller maps with NoHandlerFoundException, which Forward's
// ForwardHandlerExceptionResolver writes as a 404 JSON body whose message is "No endpoint <METHOD> <URL>." and which has
// no reason code. A controller excluded by a deployment profile looks exactly like this, so callers need it told apart
// from "the thing is missing" -- especially on a network path, where a network 404 would otherwise be assumed.
func TestEndpointNotServedIsClassifiedBeforeNetworkNotFound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   ErrorKind
	}{
		{"unmapped route", http.MethodGet, "/api/orgs/7/licenses",
			`{"apiUrl":"/api/orgs/7/licenses","httpMethod":"GET","message":"No endpoint GET /api/orgs/7/licenses."}`, ErrorKindEndpointNotServed},
		{"unmapped route under a network is not a missing network", http.MethodGet, "/api/networks/9/snapshots",
			`{"apiUrl":"/api/networks/9/snapshots","httpMethod":"GET","message":"No endpoint GET /api/networks/9/snapshots."}`, ErrorKindEndpointNotServed},
		{"method comes from the request when the body omits it", http.MethodPost, "/api/backups",
			`{"message":"No endpoint POST /api/backups."}`, ErrorKindEndpointNotServed},
		{"a real missing network is still a missing network", http.MethodGet, "/api/networks/9/snapshots",
			`{"apiUrl":"/api/networks/9/snapshots","httpMethod":"GET","message":"Network 9 not found"}`, ErrorKindNetworkNotFound},
		{"an empty 404 under a network is still a missing network", http.MethodGet, "/api/networks/9",
			`{}`, ErrorKindNetworkNotFound},
		{"another method's message does not match", http.MethodGet, "/api/orgs/7/licenses",
			`{"message":"No endpoint POST /api/orgs/7/licenses."}`, ErrorKindUnknown},
		{"a reason code means Forward chose this answer", http.MethodGet, "/api/orgs/7/licenses",
			`{"reason":"SOMETHING","message":"No endpoint GET /api/orgs/7/licenses."}`, ErrorKindUnknown},
		{"other 404 text is not it", http.MethodGet, "/api/orgs/7/licenses",
			`{"message":"Org 7 not found"}`, ErrorKindUnknown},
		{"a 400 with the text is not it", http.MethodGet, "/api/orgs/7/licenses",
			`{"message":"No endpoint GET /api/orgs/7/licenses."}`, ErrorKindUnknown},
	}
	for _, tc := range cases {
		status := http.StatusNotFound
		if tc.name == "a 400 with the text is not it" {
			status = http.StatusBadRequest
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, tc.body)
		}))
		client := newTestClient(t, server.URL)
		req, err := client.NewRequest(context.Background(), tc.method, tc.path, nil)
		if err != nil {
			server.Close()
			t.Fatalf("%s: %v", tc.name, err)
		}
		_, err = client.Do(req, nil)
		server.Close()
		var apiErr *ErrorResponse
		if !errors.As(err, &apiErr) {
			t.Errorf("%s: err = %v, want an *ErrorResponse", tc.name, err)
			continue
		}
		if apiErr.Kind != tc.want {
			t.Errorf("%s: kind = %q, want %q", tc.name, apiErr.Kind, tc.want)
		}
		if got := errors.Is(err, ErrEndpointNotServed); got != (tc.want == ErrorKindEndpointNotServed) {
			t.Errorf("%s: errors.Is(ErrEndpointNotServed) = %v", tc.name, got)
		}
		if tc.want == ErrorKindEndpointNotServed && (errors.Is(err, ErrNetworkNotFound) || !IsStatus(err, http.StatusNotFound)) {
			t.Errorf("%s: must stay a 404 and not read as a missing network", tc.name)
		}
	}
}
