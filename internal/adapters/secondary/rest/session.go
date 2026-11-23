package rest

import (
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// AuthMode is the principal this client acts as (ports.Session).
func (c *Client) AuthMode() ports.AuthMode {
	if c == nil {
		return ""
	}
	return c.authMode
}

// Unavailable returns the *ports.UnavailableError of a client from NewUnavailable, and nil for any other client: what a use
// case checks first, so a fail-closed client names its cause before any argument check (the v0.9 hexagonal split).
func (c *Client) Unavailable() error {
	if c == nil || c.unavailableErr == nil {
		return nil
	}
	return &ports.UnavailableError{NetworkID: c.networkID, Cause: c.unavailableErr}
}
