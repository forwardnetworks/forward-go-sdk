package ports

import (
	"net/url"
)

// OrganizationCreateRequest contains known administrative query fields.
// ExtraQuery carries fields introduced by a particular Forward version.
type OrganizationCreateRequest struct {
	Name          string
	Type          string
	OnPrem        *bool
	SFDCAccountID string
	SFDCContactID string
	ExtraQuery    url.Values
}
