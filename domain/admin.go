package domain

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type FlexibleTimestamp struct{ time.Time }

func (t *FlexibleTimestamp) UnmarshalJSON(raw []byte) error {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		t.Time = time.Time{}
		return nil
	}
	if strings.HasPrefix(value, `"`) {
		var decoded string
		if json.Unmarshal(raw, &decoded) != nil {
			t.Time = time.Time{}
			return nil
		}
		value = strings.TrimSpace(decoded)
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			t.Time = parsed.UTC()
			return nil
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		t.Time = time.Time{}
		return nil
	}
	if n >= 1e11 {
		t.Time = time.UnixMilli(n).UTC()
	} else {
		t.Time = time.Unix(n, 0).UTC()
	}
	return nil
}

type AdminNetwork struct {
	ID        Identifier        `json:"id"`
	Name      string            `json:"name"`
	OrgID     Identifier        `json:"orgId"`
	OrgName   string            `json:"orgName"`
	CreatorID Identifier        `json:"creatorId"`
	Creator   string            `json:"creator"`
	CreatedAt FlexibleTimestamp `json:"createdAt"`
}

type AdminUser struct {
	ID       Identifier `json:"id"`
	OrgID    Identifier `json:"orgId"`
	Username string     `json:"username"`
	Email    string     `json:"email"`
	// AuthSource names where the user authenticates (local, saml, ldap).
	AuthSource string `json:"authSource,omitempty"`
	// ExternalGroups is the IdP group membership Forward derives ACG
	// membership from. A LOCAL user has none and can never join an ACG, which
	// is why device-access restrictions cannot apply to local accounts.
	ExternalGroups []string `json:"externalGroups,omitempty"`
}

type AdminUserCreateRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
	Enabled  bool   `json:"enabled"`
}

type AdminUserPatch struct {
	Password *string `json:"password,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
}

// FDBUsage is the stored size under an FDB prefix, as Forward's usage trie lists it (GET /api/system/fdb/usage?prefix=). Sizes are left
// raw: the shape is Forward's own debug output.
type FDBUsage struct {
	Sizes json.RawMessage `json:"sizes"`
}
