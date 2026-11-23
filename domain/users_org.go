package domain

// NetworkRole is a role a user holds on one network. The values are Forward's
// NetworkRole enum; only NetworkRoleAdmin carries UPLOAD_SNAPSHOT, and
// NetworkRoleOperator carries MANAGE_CHANGE_SETS but not upload.
type NetworkRole string

const (
	NetworkRoleLimitedReadOnly   NetworkRole = "LIMITED_READ_ONLY"
	NetworkRoleReadOnly          NetworkRole = "READ_ONLY"
	NetworkRoleOperator          NetworkRole = "OPERATOR"
	NetworkRoleWorkspaceOperator NetworkRole = "WORKSPACE_OPERATOR"
	NetworkRoleAdmin             NetworkRole = "ADMIN"
)

func (r NetworkRole) Valid() bool {
	switch r {
	case NetworkRoleLimitedReadOnly, NetworkRoleReadOnly, NetworkRoleOperator, NetworkRoleWorkspaceOperator, NetworkRoleAdmin:
		return true
	}
	return false
}

// UserCreateRequest is the body of POST /api/users. Username defaults to
// Email on the server when empty.
type UserCreateRequest struct {
	Email    string `json:"email"`
	Username string `json:"username,omitempty"`
	Password string `json:"password"`
	Enabled  *bool  `json:"enabled,omitempty"`
	// IsSupport marks a support account (NewUser.isSupport).
	IsSupport *bool `json:"isSupport,omitempty"`
}

// UserWithRoles is a user with its access summary (UserAccountWithRoles, GET /api/users?view=roles). AccessibleNetworks lists each
// network the user can reach and its role there; the API-token times are omitted when the user has no viable token.
type UserWithRoles struct {
	User
	OrgAdmin                bool                `json:"orgAdmin"`
	ForwardAdmin            bool                `json:"fnAdmin,omitempty"`
	SupportAdmin            bool                `json:"supportAdmin,omitempty"`
	AccessibleNetworks      []AccessibleNetwork `json:"accessibleNetworks,omitempty"`
	SupportedOrgIDs         []string            `json:"supportedOrgIds,omitempty"`
	GroupIDs                []string            `json:"groupIds,omitempty"`
	OldestAPITokenCreatedAt string              `json:"oldestApiTokenCreatedAt,omitempty"`
	APITokenLastUsedAt      string              `json:"apiTokenLastUsedAt,omitempty"`
}

// AccessibleNetwork is one network a user can reach, with the role it holds there.
type AccessibleNetwork struct {
	ID   Identifier `json:"id"`
	Name string     `json:"name"`
	Role string     `json:"role"`
}

// TwoFactorStatus is one user's two-factor setup.
type TwoFactorStatus struct {
	SetUp             bool `json:"setUp"`
	AnyTrustedDevices bool `json:"anyTrustedDevices"`
}

// UserPatch changes some of a user; nil fields are left alone (UserPatch on the server).
type UserPatch struct {
	Email     *string `json:"email,omitempty"`
	Username  *string `json:"username,omitempty"`
	Password  *string `json:"password,omitempty"`
	IsSupport *bool   `json:"isSupport,omitempty"`
	Enabled   *bool   `json:"enabled,omitempty"`
}
