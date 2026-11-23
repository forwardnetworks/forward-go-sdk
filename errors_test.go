package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

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
	for status, want := range map[int]domain.ErrorKind{http.StatusBadRequest: domain.ErrorKindActionNotSupported, http.StatusConflict: domain.ErrorKindUnknown} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"Parameter conditions \"action=bulkAdd\" not met for actual request parameters: action={add}"}`))
		}))
		client := newTestClient(t, server.URL)
		_, _, err := client.Networks.List(context.Background())
		server.Close()
		if !ports.IsErrorKind(err, want) {
			t.Fatalf("status %d: kind = %v, want %v", status, err, want)
		}
		if want == domain.ErrorKindActionNotSupported && !errors.Is(err, domain.ErrActionNotSupported) {
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
		want    domain.ErrorKind
	}{
		{http.StatusConflict, "ServiceNow instance is hibernating", domain.ErrorKindServiceNowInstanceAsleep},
		{http.StatusConflict, "ServiceNow instance is suspended.", domain.ErrorKindServiceNowInstanceAsleep},
		{http.StatusConflict, "Unknown Error", domain.ErrorKindUnknown},
		{http.StatusBadGateway, "ServiceNow instance is hibernating", domain.ErrorKindUnknown},
		{http.StatusConflict, "The ServiceNow instance is hibernating, retry", domain.ErrorKindUnknown},
	}
	for _, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"message":"` + tc.message + `"}`))
		}))
		_, _, err := newTestClient(t, server.URL).Networks.List(context.Background())
		server.Close()
		if got := ports.IsErrorKind(err, tc.want); !got {
			t.Errorf("%d %q: kind is not %v: %v", tc.status, tc.message, tc.want, err)
		}
		if asleep := errors.Is(err, domain.ErrServiceNowInstanceAsleep); asleep != (tc.want == domain.ErrorKindServiceNowInstanceAsleep) {
			t.Errorf("%d %q: ErrServiceNowInstanceAsleep = %v", tc.status, tc.message, asleep)
		}
	}
}

// Locations.Assign's whole-patch refusal names the devices Forward does not know; they come out of the message in order.
func TestUnrecognizedDevicesKind(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Unrecognized devices cannot be moved: [r1, r2]"}`))
	}))
	defer server.Close()
	_, err := newTestClient(t, server.URL).Locations.Assign(context.Background(), "n1", domain.AtlasPatch{"r1": "L1", "r2": "L1"})
	var apiErr *ports.ErrorResponse
	if !errors.Is(err, domain.ErrUnrecognizedDevices) || !errors.As(err, &apiErr) || len(apiErr.UnrecognizedDevices) != 2 || apiErr.UnrecognizedDevices[1] != "r2" {
		t.Fatalf("err = %v, devices = %v", err, apiErr)
	}
}
