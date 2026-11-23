package domain

import (
	"encoding/json"
)

type BrowserUser struct {
	ID              Identifier `json:"id"`
	OrgID           Identifier `json:"orgId"`
	Username        string     `json:"username"`
	Email           string     `json:"email"`
	MustSetPassword bool       `json:"mustSetPassword"`
	AuthSource      string     `json:"authSource,omitempty"`
	ExternalGroups  []string   `json:"externalGroups,omitempty"`
}

// BrowserSessionRoles are the roles Forward resolved for the session's
// principal: org roles (e.g. ADMIN) and network roles keyed by network ID.
type BrowserSessionRoles struct {
	Org     []string          `json:"org"`
	Network map[string]string `json:"network"`
}

// UnmarshalJSON reads the roles as SessionRoles does: org may be a list, a bare string or null, and a network's role a string or a
// one-element list.
func (r *BrowserSessionRoles) UnmarshalJSON(data []byte) error {
	var raw struct {
		Org     json.RawMessage            `json:"org"`
		Network map[string]json.RawMessage `json:"network"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	org, err := roleList("org", raw.Org)
	if err != nil {
		return err
	}
	network, err := singleNetworkRoles(raw.Network)
	if err != nil {
		return err
	}
	r.Org, r.Network = org, network
	return nil
}

// BrowserSession is GET /api/users/current in full: who the session is and
// what Forward lets it do. For an impersonated session it is the target.
type BrowserSession struct {
	User  BrowserUser         `json:"user"`
	Roles BrowserSessionRoles `json:"roles"`
}

type BrowserCSRFToken struct {
	HeaderName    string `json:"headerName"`
	ParameterName string `json:"parameterName"`
	Token         string `json:"token"`
}

type BrowserLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
