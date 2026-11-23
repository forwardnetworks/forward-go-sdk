package domain

// OrgHTTPCredential is a credential in the org-level HTTP store (StoredHttpCredential: the credential plus its attribution). Password is the
// id of the stored secret, not the secret. Username, LoginType and AutoAssociate apply to LOGIN credentials only.
type OrgHTTPCredential struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	Username      string `json:"username,omitempty"`
	PasswordID    string `json:"password"`
	LoginType     string `json:"loginType,omitempty"`
	AutoAssociate *bool  `json:"autoAssociate,omitempty"`
	CreatedByID   string `json:"createdById,omitempty"`
	CreatedAt     string `json:"createdAt,omitempty"`
	CreatedBy     string `json:"createdBy,omitempty"`
	UpdatedByID   string `json:"updatedById,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
	UpdatedBy     string `json:"updatedBy,omitempty"`
}
