package forward

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A locked login or access key answers 401 with the LockedException message in
// ErrorInfo.message ("User account is locked: <name>", "Account is locked").
// That is not a wrong password: retrying or rotating credentials does nothing
// until the lock lapses, and every further attempt is refused, so a caller must
// be able to tell it apart from an ordinary 401 -- while it still matches
// ErrAuthentication, which every 401 matched before.
func TestAccountLockedIsDistinctFromAWrongCredential(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		message string
		locked  bool
	}{
		// The browser form login's JsonAuthenticationFailureHandler answers {"error":"locked"} and no message.
		{"", true},
		{"User account is locked: svc-forward", true},
		{"Account is locked", true},
		{"Bad credentials", false},
		{"User is disabled: svc-forward", false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			body := `{"apiUrl":"/api/version","httpMethod":"GET","message":"` + tc.message + `"}`
			if tc.message == "" {
				body = `{"error":"locked"}`
			}
			_, _ = w.Write([]byte(body))
		}))
		client := newTestClient(t, server.URL)
		_, _, err := client.Networks.List(context.Background())
		server.Close()
		if err == nil {
			t.Fatalf("%q: want an error", tc.message)
		}
		if got := errors.Is(err, ErrAccountLocked); got != tc.locked {
			t.Errorf("%q: Is(ErrAccountLocked) = %v, want %v", tc.message, got, tc.locked)
		}
		var apiErr *ErrorResponse
		if errors.As(err, &apiErr) && (apiErr.Kind == ErrorKindAccountLocked) != tc.locked {
			t.Errorf("%q: Kind = %q", tc.message, apiErr.Kind)
		}
		if !errors.Is(err, ErrAuthentication) {
			t.Errorf("%q: a 401 must still match ErrAuthentication", tc.message)
		}
	}
}
