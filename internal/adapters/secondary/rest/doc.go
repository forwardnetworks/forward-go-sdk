// Package rest is the SDK's HTTP adapter (the v0.9 hexagonal split): it implements every port in package ports against
// Forward's REST API. It holds the transport, the credentials, the rate-limit cooldown, the CSRF state and the capability
// registry, and it imports nothing of this module but ports. Only the root's composition-root.go imports it.
package rest
