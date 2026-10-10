package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The appserver fills apiUrl with the request URI AND its query string
// (RequestUtils.getRequestString), so a real refusal says
// "/api/collector-tasks?networkId=5323&type=NETWORK_COLLECTION". Route checks that
// compared apiUrl to a bare path never matched a live response, and a running
// collection surfaced as an unclassified 400. Fixtures that omitted apiUrl hid it.
func TestCollectionInFlightClassifiedWhenAPIURLCarriesTheQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"apiUrl":"/api/collector-tasks?networkId=5323&type=NETWORK_COLLECTION","httpMethod":"POST","message":"Collection in network 5323 is already in progress"}`))
	}))
	defer srv.Close()

	_, _, err := newTestClient(t, srv.URL).CollectorTasks.Start(context.Background(), "5323")
	if !IsCollectionAlreadyInProgress(err) {
		t.Fatalf("Start() error = %v, want ErrCollectionAlreadyInProgress", err)
	}
}
