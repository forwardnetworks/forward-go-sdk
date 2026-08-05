package forward

import (
	"bytes"
	"context"
	"crypto/tls"
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

func TestNewRequestAuthenticationAndURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      Config
		wantUser string
		wantPass string
	}{
		{
			name:     "username password",
			cfg:      Config{BaseURL: "https://fwd.example/api/", Username: "user@example.com", Password: "pass"},
			wantUser: "user@example.com",
			wantPass: "pass",
		},
		{
			name:     "API token",
			cfg:      Config{BaseURL: "https://fwd.example", APIToken: "access:secret:with-colon"},
			wantUser: "access",
			wantPass: "secret:with-colon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := NewClient(tt.cfg)
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			req, err := client.NewRequest(context.Background(), http.MethodGet, "/api/networks?limit=1", nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			if got := req.URL.String(); got != "https://fwd.example/api/networks?limit=1" {
				t.Fatalf("request URL = %q", got)
			}
			user, pass, ok := req.BasicAuth()
			if !ok || user != tt.wantUser || pass != tt.wantPass {
				t.Fatalf("BasicAuth() = %q, %q, %v", user, pass, ok)
			}
			if got := req.Header.Get("User-Agent"); got != defaultUserAgent {
				t.Fatalf("User-Agent = %q", got)
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

func TestNewClientDoesNotMutateSuppliedHTTPClient(t *testing.T) {
	t.Parallel()

	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}
	httpClient := &http.Client{Transport: transport}
	client, err := NewClient(Config{
		BaseURL:            "https://fwd.example",
		Username:           "user",
		Password:           "pass",
		HTTPClient:         httpClient,
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("NewClient() mutated the supplied transport")
	}
	configured := client.httpClient.Transport.(*http.Transport)
	if !configured.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("configured transport does not skip TLS verification")
	}
	if configured == transport || configured.TLSClientConfig == transport.TLSClientConfig {
		t.Fatal("configured transport was not cloned")
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
	var version APIVersion
	resp, err := client.Do(req, &version)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK || version.Version != "26.30.0" {
		t.Fatalf("Do() response = %d, version = %#v", resp.StatusCode, version)
	}

	req, _ = client.NewRequest(context.Background(), http.MethodPost, "/api/fail", nil)
	resp, err = client.Do(req, nil)
	if !IsStatus(err, http.StatusConflict) {
		t.Fatalf("Do() error = %v, want status 409", err)
	}
	if resp == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("Do() response = %#v", resp)
	}
	var apiErr *ErrorResponse
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
	if !IsStatus(err, http.StatusFound) || resp == nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("Do() = %#v, %v; want original redirect response", resp, err)
	}
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target received %d requests", got)
	}
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := NewClient(Config{BaseURL: baseURL, Username: "user", Password: "pass"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}
