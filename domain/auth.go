package domain

// AuthMode identifies the principal used for a Forward operation. User and
// service principals both use HTTP Basic authentication on the wire, but stay
// distinct so factories, policy, and telemetry cannot silently mix them.
type AuthMode string

const (
	AuthModeUser      AuthMode = "user"
	AuthModeService   AuthMode = "service"
	AuthModeCollector AuthMode = "collector"
	AuthModeBrowser   AuthMode = "browser"
	AuthModeNone      AuthMode = "none"
)

// CollectorIdentity is the authorization identity returned by collector
// registration. AuthorizationKey is a collector secret; it is not the API
// user's password or token secret.
type CollectorIdentity struct {
	Username         string `json:"username"`
	AuthorizationKey string `json:"authorizationKey"`
}
