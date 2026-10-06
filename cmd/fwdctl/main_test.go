package main

import "testing"

// Certificate checks may be skipped only for a Forward on this machine; any other host must be verified unless the
// caller says --insecure.
func TestIsLoopbackURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://localhost:8443": true, "https://127.0.0.1": true, "https://[::1]:8443": true,
		"https://fwd.app": false, "https://localhost.example.com": false, "::bad": false,
	} {
		if got := isLoopbackURL(raw); got != want {
			t.Errorf("isLoopbackURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
