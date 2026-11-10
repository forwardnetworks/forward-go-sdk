package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A build before Forward 26.11 maps only PATCH and DELETE on /api/networks/{id}
// and answers a GET 405 with an HTML page (seen live on 26.10.5), so Networks.Get
// reads the network from the listing there. A genuine 404 (an expired network on
// 26.11) is not a missing route and must not be papered over by the fallback.
func TestNetworksGetFallsBackToTheListingOnlyWhenTheRouteIsMissing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		body   string
		wantOK bool
	}{
		{"405 html", 405, "<html>Method Not Allowed</html>", true},
		{"no endpoint 404", 404, `{"httpStatusCode":404,"message":"No endpoint GET /api/networks/n2."}`, true},
		{"real 404", 404, `{"httpStatusCode":404,"message":"Network n2 not found"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var listed bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/networks" {
					listed = true
					_, _ = io.WriteString(w, `[{"id":"n1","name":"A"},{"id":"n2","name":"B"}]`)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			network, _, err := newTestClient(t, server.URL).Networks.Get(context.Background(), "n2")
			if tc.wantOK && (err != nil || network.ID != "n2" || network.Name != "B") {
				t.Fatalf("Get = %+v, %v", network, err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("a real 404 was turned into a success")
			}
			if listed != tc.wantOK {
				t.Fatalf("listing read = %v, want %v", listed, tc.wantOK)
			}
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/networks" {
			_, _ = io.WriteString(w, `[{"id":"n1","name":"A"}]`)
			return
		}
		w.WriteHeader(405)
	}))
	defer server.Close()
	if _, _, err := newTestClient(t, server.URL).Networks.Get(context.Background(), "zzz"); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("a network missing from the listing = %v, want ErrNetworkNotFound", err)
	}
}
