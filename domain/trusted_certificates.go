package domain

import (
	"errors"
	"strings"
	"time"
)

// TrustedCertificate is a stored trusted certificate (StoredTrustedCertificate
// on the wire, with TrustedCertificate and Attribution JSON-unwrapped into one
// object per @JsonUnwrapped on both fields). CreatedBy/UpdatedBy are usernames
// (augmented server-side); they are empty when the acting user was deleted or
// no update has happened yet, matching AttributionWithNames' NON_NULL fields.
type TrustedCertificate struct {
	Name        string    `json:"name"`
	Certificate string    `json:"certificate"`
	ExpiresAt   time.Time `json:"expiresAt,omitempty"`
	CreatedBy   string    `json:"createdBy,omitempty"`
	UpdatedBy   string    `json:"updatedBy,omitempty"`
}

// NewTrustedCertificateRequest is the POST body (NewTrustedCertificate.java):
// exactly {name, certificate}, both required.
type NewTrustedCertificateRequest struct {
	Name        string `json:"name"`
	Certificate string `json:"certificate"`
}

func (r NewTrustedCertificateRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("forward: trusted certificate name is required")
	}
	if strings.TrimSpace(r.Certificate) == "" {
		return errors.New("forward: trusted certificate PEM content is required")
	}
	return nil
}
