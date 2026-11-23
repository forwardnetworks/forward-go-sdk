package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// Org-level HTTP credentials (CredentialController GET/POST /http-credentials and GET/PATCH/DELETE /http-credentials/{credentialId},
// backed by OrgHttpCredentialService; Forward origin/master). Read from source; the only live observation is the scanner refusal quoted
// below, seen on 26.11.0-03 by forward-skills. The routes themselves were not run live by the SDK. Preview: not in the published spec.
//
// These are a different store from the per-network ones (Credentials.CreateHTTP and the other *HTTP methods take a network id): a
// credential made on a network is invisible here, and an id from here is not a network credential. The org-level store exists for the
// org-level vulnerability scanners: Forward refuses a scanner whose credentialId is not in it ("HTTP credential H-0 does not exist"),
// or is not of type LOGIN ("HTTP credential ... must be of type LOGIN", VulnerabilityScannerService at origin/master). The "does not exist"
// refusal was seen live on 26.11.0-03; the LOGIN one is from source only.
//
// Permissions are org-level: VIEW_CREDENTIALS to read, EDIT_CREDENTIALS to create, change or delete. The request bodies are not logged by
// Forward. The secret is never returned: the stored credential's password field is a secret id, not the secret.

func orgHTTPCredentialPath(credentialID string) (string, error) {
	credentialID = strings.TrimSpace(credentialID)
	if credentialID == "" {
		return "", errors.New("forward: credential ID is required")
	}
	return "/api/http-credentials/" + url.PathEscape(credentialID), nil
}

// checkOrgHTTPCredential refuses what Forward's HttpCredential constructor (client-structs HttpCredential.java) refuses; if Forward relaxes
// that rule, this check must be relaxed too: a LOGIN credential (the default when Type is empty) needs a
// username with no spaces, and an API_KEY one takes no username, login type or auto-associate.
func checkOrgHTTPCredential(c ports.HTTPCredentialRequest) error {
	if err := validateCredential(c.Type, c.Name, c.Password); err != nil {
		return err
	}
	if c.Type == "" || c.Type == "LOGIN" {
		if c.Username == "" || strings.Contains(c.Username, " ") {
			return errors.New("forward: a LOGIN credential needs a username without spaces")
		}
		return nil
	}
	if c.Username != "" || c.LoginType != "" || c.AutoAssociate != nil {
		return errors.New("forward: username, loginType and autoAssociate apply to LOGIN credentials only")
	}
	return nil
}

// ListOrgHTTP lists the organization's HTTP credentials (GET /api/http-credentials; org VIEW_CREDENTIALS).
func (s *Credentials) ListOrgHTTP(ctx context.Context) ([]ports.OrgHTTPCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/http-credentials", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Credentials.ListOrgHTTP")
	var out []ports.OrgHTTPCredential
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// CreateOrgHTTP adds an HTTP credential to the organization (POST /api/http-credentials; org EDIT_CREDENTIALS). The body is the one the
// network-level CreateHTTP sends (NewHttpCredential): an empty Type is LOGIN, and Name and Password are required. For a Tenable VM scanner
// make a LOGIN credential with the access key as Username and the secret key as Password. Forward assigns the id and answers 400 if the
// name is already taken in the org.
func (s *Credentials) CreateOrgHTTP(ctx context.Context, credential ports.HTTPCredentialRequest) (*ports.OrgHTTPCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := checkOrgHTTPCredential(credential); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/http-credentials", credential)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Credentials.CreateOrgHTTP")
	out := new(ports.OrgHTTPCredential)
	resp, err := s.client.Do(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// GetOrgHTTP reads one credential (GET /api/http-credentials/{credentialId}; org VIEW_CREDENTIALS). An unknown id is a 404.
func (s *Credentials) GetOrgHTTP(ctx context.Context, credentialID string) (*ports.OrgHTTPCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := orgHTTPCredentialPath(credentialID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Credentials.GetOrgHTTP")
	out := new(ports.OrgHTTPCredential)
	resp, err := s.client.Do(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// UpdateOrgHTTP changes part of a credential (PATCH /api/http-credentials/{credentialId}; org EDIT_CREDENTIALS) and returns the stored
// credential, unlike the network-level update, which answers 204. Nil fields are not sent, so a field cannot be cleared with this call (an
// explicit empty string is a 400 for a username). Forward answers 400 for a name already taken.
func (s *Credentials) UpdateOrgHTTP(ctx context.Context, credentialID string, patch ports.HTTPCredentialPatch) (*ports.OrgHTTPCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := orgHTTPCredentialPath(credentialID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Credentials.UpdateOrgHTTP")
	out := new(ports.OrgHTTPCredential)
	resp, err := s.client.Do(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// DeleteOrgHTTP removes a credential (DELETE /api/http-credentials/{credentialId}; org EDIT_CREDENTIALS; 204). At origin/master Forward
// refuses it with 400 while a vulnerability scanner uses it (OrgHttpCredentialService.validateCredentialUnused: "HTTP Credential ... is
// used by N sources"); an older Forward build may not have that check, so do not rely on it as a safety net. An unknown id is a 404. Delete* name: Skyforge's destructive-SDK catalogue must list it.
func (s *Credentials) DeleteOrgHTTP(ctx context.Context, credentialID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := orgHTTPCredentialPath(credentialID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Credentials.DeleteOrgHTTP"), nil)
}
