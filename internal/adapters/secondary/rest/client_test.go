package rest

import (
	"context"
	"crypto/tls"
	"net/http"
	"testing"
)

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

// privateHTTPClient returns an HTTP client with a transport of its own. httptest.Server.Close calls
// CloseIdleConnections on http.DefaultTransport, which fails other parallel tests' in-flight requests
// ("http: CloseIdleConnections called") if they share it, so a test that builds its own Client should not.
func privateHTTPClient() *http.Client {
	return &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone(), Timeout: defaultTimeout}
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
		HTTPClient: &http.Client{Transport: transport, Timeout: defaultTimeout},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}
