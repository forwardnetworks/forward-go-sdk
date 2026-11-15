package forward

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The AutoCon6 workshop set advanced_reachability_analysis to ASYNC on 67 networks through these routes on 26.10.6: the value rides in
// the query, the answer maps the property to its parsed value, clearing is a DELETE, and the refusals it saw classify.
func TestNetworkPropertySetAndClear(t *testing.T) {
	var method, uri string
	status, message := http.StatusOK, ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, uri = r.Method, r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		if message != "" {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"` + message + `"}`))
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"advanced_reachability_analysis":"ASYNC"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	out, _, err := client.Properties.SetNetwork(ctx, "5744", NetworkPropertyAdvancedReachabilityAnalysis, AdvancedReachabilityAsync)
	if err != nil || string(out["advanced_reachability_analysis"]) != `"ASYNC"` {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if method != http.MethodPut || uri != "/api/networks/5744/config/advanced_reachability_analysis?value=ASYNC" {
		t.Fatalf("sent %s %s", method, uri)
	}
	if _, err = client.Properties.ClearNetwork(ctx, "5744", NetworkPropertyAdvancedReachabilityAnalysis); err != nil || method != http.MethodDelete {
		t.Fatalf("clear: %s err=%v", method, err)
	}
	if _, _, err = client.Properties.SetNetwork(ctx, "5744", "", "x"); err == nil {
		t.Fatal("a missing property name must be refused before any request")
	}

	status, message = http.StatusBadRequest, "No enum constant com.forwardnetworks.cv.config.NetworkProperty.LOCATION_CONNECTIVITY_DIFFS"
	_, _, err = client.Properties.SetNetwork(ctx, "5744", "location_connectivity_diffs", "true")
	if !IsUnknownNetworkProperty(err) || IsUnknownOrgProperty(err) {
		t.Fatalf("an org-only property written per network must read as an unknown network property: %v", err)
	}
	status, message = http.StatusForbidden, "Org 7 is not accessible to user 12"
	_, _, err = client.Properties.Organization(ctx, "7", PropertyFilterOff)
	if !IsOrgNotAccessible(err) || !errors.Is(err, ErrOrgNotAccessible) {
		t.Fatalf("a support key refused an org must read as not accessible: %v", err)
	}
	// Only the whole message classifies, and only as a 403.
	status, message = http.StatusForbidden, "Org 7 is not accessible to user 12, ask an admin"
	if _, _, err = client.Properties.Organization(ctx, "7", PropertyFilterOff); IsOrgNotAccessible(err) {
		t.Fatal("a message that merely contains the sentence must not classify")
	}
}
