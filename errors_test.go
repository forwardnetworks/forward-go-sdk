package forward

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Spring's 400 for an unmapped ?action= is the only signal a build lacks a variant; it must classify without callers matching text,
// and only as a 400.
func TestActionNotSupportedKind(t *testing.T) {
	for status, want := range map[int]ErrorKind{http.StatusBadRequest: ErrorKindActionNotSupported, http.StatusConflict: ErrorKindUnknown} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"Parameter conditions \"action=bulkAdd\" not met for actual request parameters: action={add}"}`))
		}))
		client := newTestClient(t, server.URL)
		_, _, err := client.Networks.List(context.Background())
		server.Close()
		if !IsErrorKind(err, want) {
			t.Fatalf("status %d: kind = %v, want %v", status, err, want)
		}
		if want == ErrorKindActionNotSupported && !errors.Is(err, ErrActionNotSupported) {
			t.Fatal("ErrActionNotSupported must match")
		}
	}
}

// forward-skills saw a 409 "ServiceNow instance is hibernating" live on 26.10.6 when AddCheckIncident hit a sleeping developer instance.
// That text classifies; the 409 "Unknown Error" a woken instance answered does not, and neither does the text under another status.
func TestServiceNowAsleepKind(t *testing.T) {
	cases := []struct {
		status  int
		message string
		want    ErrorKind
	}{
		{http.StatusConflict, "ServiceNow instance is hibernating", ErrorKindServiceNowInstanceAsleep},
		{http.StatusConflict, "ServiceNow instance is suspended.", ErrorKindServiceNowInstanceAsleep},
		{http.StatusConflict, "Unknown Error", ErrorKindUnknown},
		{http.StatusBadGateway, "ServiceNow instance is hibernating", ErrorKindUnknown},
		{http.StatusConflict, "The ServiceNow instance is hibernating, retry", ErrorKindUnknown},
	}
	for _, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"message":"` + tc.message + `"}`))
		}))
		_, _, err := newTestClient(t, server.URL).Networks.List(context.Background())
		server.Close()
		if got := IsErrorKind(err, tc.want); !got {
			t.Errorf("%d %q: kind is not %v: %v", tc.status, tc.message, tc.want, err)
		}
		if asleep := errors.Is(err, ErrServiceNowInstanceAsleep); asleep != (tc.want == ErrorKindServiceNowInstanceAsleep) {
			t.Errorf("%d %q: ErrServiceNowInstanceAsleep = %v", tc.status, tc.message, asleep)
		}
	}
}
