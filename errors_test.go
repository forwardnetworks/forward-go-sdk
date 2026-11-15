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
