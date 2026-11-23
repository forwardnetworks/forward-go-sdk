package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

import (
	"bytes"
	"context"

	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNewClientValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "missing URL", cfg: Config{Username: "user", Password: "pass"}, want: "base URL is required"},
		{name: "missing scheme", cfg: Config{BaseURL: "fwd.example", Username: "user", Password: "pass"}, want: "must use http or https"},
		{name: "URL credentials", cfg: Config{BaseURL: "https://user:pass@fwd.example", Username: "user", Password: "pass"}, want: "must not include credentials"},
		{name: "URL path", cfg: Config{BaseURL: "https://fwd.example/other", Username: "user", Password: "pass"}, want: "path must be empty or /api"},
		{name: "missing auth", cfg: Config{BaseURL: "https://fwd.example"}, want: "username and password are required"},
		{name: "mixed auth", cfg: Config{BaseURL: "https://fwd.example", Username: "user", Password: "pass", APIToken: "key:secret"}, want: "either API token or username/password"},
		{name: "invalid token", cfg: Config{BaseURL: "https://fwd.example", APIToken: "token"}, want: "accessKey:secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewClient(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewClient() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestNewRequestRejectsAbsoluteURL(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, "https://fwd.example")
	_, err := client.NewRequest(context.Background(), http.MethodGet, "https://attacker.example/api/networks", nil)
	if err == nil || !strings.Contains(err.Error(), "must not be an absolute URL") {
		t.Fatalf("NewRequest() error = %v", err)
	}
}

func TestDoDecodesSuccessAndError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"version":"26.30.0"}`)
		case "/api/fail":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"httpMethod":"POST","apiUrl":"/api/fail","message":"conflict","reason":"busy"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	req, _ := client.NewRequest(context.Background(), http.MethodGet, "/api/version", nil)
	var version domain.APIVersion
	resp, err := client.Do(req, &version)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK || version.Version != "26.30.0" {
		t.Fatalf("Do() response = %d, version = %#v", resp.StatusCode, version)
	}

	req, _ = client.NewRequest(context.Background(), http.MethodPost, "/api/fail", nil)
	resp, err = client.Do(req, nil)
	if !ports.IsStatus(err, http.StatusConflict) {
		t.Fatalf("Do() error = %v, want status 409", err)
	}
	if resp == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("Do() response = %#v", resp)
	}
	var apiErr *ports.ErrorResponse
	if !errors.As(err, &apiErr) || apiErr.Message != "conflict" || apiErr.Reason != "busy" {
		t.Fatalf("Do() ErrorResponse = %#v", apiErr)
	}
}

func TestDoCopiesToWriter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "snapshot bytes")
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	req, _ := client.NewRequest(context.Background(), http.MethodGet, "/api/snapshots/id", nil)
	var dst bytes.Buffer
	if _, err := client.Do(req, &dst); err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if got := dst.String(); got != "snapshot bytes" {
		t.Fatalf("copied body = %q", got)
	}
}

func TestClientRejectsCrossOriginRedirects(t *testing.T) {
	t.Parallel()

	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetCalls.Add(1)
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL+"/api/stolen")
		w.WriteHeader(http.StatusFound)
	}))
	defer origin.Close()

	client := newTestClient(t, origin.URL)
	req, _ := client.NewRequest(context.Background(), http.MethodGet, "/api/redirect", nil)
	resp, err := client.Do(req, nil)
	if !ports.IsStatus(err, http.StatusFound) || resp == nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("Do() = %#v, %v; want original redirect response", resp, err)
	}
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target received %d requests", got)
	}
}

// privateHTTPClient returns an HTTP client with a transport of its own. httptest.Server.Close calls
// CloseIdleConnections on http.DefaultTransport, which fails other parallel tests' in-flight requests
// ("http: CloseIdleConnections called") if they share it, so a test that builds its own Client should not.
func privateHTTPClient() *http.Client {
	return &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone(), Timeout: testClientTimeout}
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	// A transport of its own: httptest.Server.Close closes idle connections on
	// http.DefaultTransport, which fails other parallel tests' in-flight requests
	// ("CloseIdleConnections called") if they share it.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	client, err := NewClient(Config{
		BaseURL:    baseURL,
		Username:   "user",
		Password:   "pass",
		HTTPClient: &http.Client{Transport: transport, Timeout: testClientTimeout},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}
