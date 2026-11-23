package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Admin service

func (s *Admin) ListNetworks(ctx context.Context) ([]ports.AdminNetwork, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.requireService(); err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.AdminNetwork]{Keys: []string{"networks", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/admin/networks", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Admin.ListNetworks")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *Admin) CreateOrganizationUser(ctx context.Context, orgID string, input ports.AdminUserCreateRequest) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := s.requireService(); err != nil {
		return nil, err
	}
	orgID = strings.TrimSpace(orgID)
	input.Email, input.Username, input.Password = strings.TrimSpace(input.Email), strings.TrimSpace(input.Username), strings.TrimSpace(input.Password)
	if orgID == "" || input.Email == "" || input.Username == "" || input.Password == "" {
		return nil, errors.New("forward: organization, email, username, and password are required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/orgs/"+url.PathEscape(orgID)+"/users", input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Admin.CreateOrganizationUser")
	return s.client.Do(req, nil)
}

func (s *Admin) GrantOrganizationAdmin(ctx context.Context, userID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := s.requireService(); err != nil {
		return nil, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("forward: user ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/users/"+url.PathEscape(userID)+"/roles/org/ADMIN", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Admin.GrantOrganizationAdmin")
	response, err := s.client.Do(req, nil)
	if ports.IsStatus(err, http.StatusConflict) {
		return response, nil
	}
	return response, err
}

func (s *Admin) LookupUser(ctx context.Context, idOrUsername string) (*ports.AdminUser, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.requireService(); err != nil {
		return nil, nil, err
	}
	idOrUsername = strings.TrimSpace(idOrUsername)
	if idOrUsername == "" {
		return nil, nil, errors.New("forward: user query is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/admin/users/"+url.PathEscape(idOrUsername), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Admin.LookupUser")
	out := new(ports.AdminUser)
	response, err := s.client.doRequired(req, out)
	if ports.IsStatus(err, http.StatusNotFound) {
		return nil, response, nil
	}
	if err == nil && out.ID == "" {
		err = errors.New("forward: user lookup returned no ID")
	}
	return out, response, err
}

func (s *Admin) ListUsers(ctx context.Context) ([]ports.AdminUser, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.requireService(); err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.AdminUser]{Keys: []string{"users", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/admin/users", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Admin.ListUsers")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *Admin) PatchUser(ctx context.Context, userID string, patch ports.AdminUserPatch) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := s.requireService(); err != nil {
		return nil, err
	}
	path, err := adminUserPath(userID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Admin.PatchUser")
	return s.client.Do(req, nil)
}

func (s *Admin) DeleteUser(ctx context.Context, userID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := s.requireService(); err != nil {
		return nil, err
	}
	path, err := adminUserPath(userID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Admin.DeleteUser")
	response, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
		return response, nil
	}
	return response, err
}

// SetSupportedOrganizations REPLACES a Forward support user's whole set of supported orgs (PUT /api/users/{id}/supported-orgs, a
// support-admin route; the user must be in the Forward org, the org named "Forward Networks", and not a Forward admin, a chatbot or
// supporting its own org). Orgs not listed are removed at once (this is also the only way to remove one: there is no per-org delete).
// There is no expiry parameter: EVERY listed org, including ones already granted, is rewritten with last-active = now and 30 days, so
// a grant made with AddSupportedOrganization for longer drops back to 30 days here. A grant stays valid for expires-in-days after the
// user's last activity in that org (each impersonation refreshes it), not after the grant was made. Read the current set with
// Users.Roles (SupportedOrgIDs) first.
func (s *Admin) SetSupportedOrganizations(ctx context.Context, userID string, orgIDs []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := s.requireService(); err != nil {
		return nil, err
	}
	path, err := adminUserPath(userID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path+"/supported-orgs", normalizeStrings(orgIDs))
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Admin.SetSupportedOrganizations")
	return s.client.Do(req, nil)
}

// AddSupportedOrganization grants or renews ONE supported org (POST /api/users/{id}/supported-orgs?orgId=&expiresIn=, a support-admin
// route with the same rules as SetSupportedOrganizations). expiresDays is how long the grant stays valid after the user's last activity
// in that org (impersonating refreshes it). Forward's own default is 30 and its maximum 365, and a value of 0 expires the grant by the end
// of the current minute; this SDK, unlike Forward, sends 365 for any value of 0 or less, so it cannot send 0 (revoke with
// SetSupportedOrganizations instead).
func (s *Admin) AddSupportedOrganization(ctx context.Context, userID, orgID string, expiresDays int) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := s.requireService(); err != nil {
		return nil, err
	}
	path, err := adminUserPath(userID)
	if err != nil {
		return nil, err
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, errors.New("forward: organization ID is required")
	}
	if expiresDays <= 0 {
		expiresDays = 365
	}
	query := url.Values{"orgId": []string{orgID}, "expiresIn": []string{strconv.Itoa(expiresDays)}}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/supported-orgs?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Admin.AddSupportedOrganization")
	return s.client.Do(req, nil)
}

func (s *Admin) requireService() error {
	if s == nil || s.client == nil {
		return ports.NilServiceError("admin")
	}
	if s.client.authMode != ports.AuthModeService {
		return errors.New("forward: admin operations require a service principal: build the client with Config{AuthMode: AuthModeService}")
	}
	return nil
}

func adminUserPath(userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", errors.New("forward: user ID is required")
	}
	return "/api/users/" + url.PathEscape(userID), nil
}

func normalizeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// FDBUsage reads the jobs-v2 FDB usage under a key prefix such as "n/<orgId>/<networkId>/" (GET /api/system/fdb/usage?prefix=).
// ADMINISTER_SYSTEM (the system ADMIN role); the prefix is required. Preview: appserver WorkersController, not in the published spec.
func (s *Admin) FDBUsage(ctx context.Context, prefix string) (*ports.FDBUsage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(prefix) == "" {
		return nil, nil, errors.New("forward: an FDB prefix is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/system/fdb/usage?"+url.Values{"prefix": []string{prefix}}.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.FDBUsage)
	resp, err := s.client.doRequired(markOperation(req, "Admin.FDBUsage"), out)
	return out, resp, err
}

// FDBSize reads the total stored size, in bytes, under an FDB key prefix (GET /api/system/fdb/size?prefix=). ADMINISTER_SYSTEM; Preview.
func (s *Admin) FDBSize(ctx context.Context, prefix string) (int64, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return 0, nil, err
	}
	if strings.TrimSpace(prefix) == "" {
		return 0, nil, errors.New("forward: an FDB prefix is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/system/fdb/size?"+url.Values{"prefix": []string{prefix}}.Encode(), nil)
	if err != nil {
		return 0, nil, err
	}
	var size int64
	resp, err := s.client.doRequired(markOperation(req, "Admin.FDBSize"), &size)
	return size, resp, err
}

// PurgeFDBGarbage starts Forward's FDB garbage collection (POST /api/system/fdb/gc; ADMINISTER_SYSTEM): VACUUM ANALYZE, vacuumlo, and
// dropping old FDB databases. It REMOVES data, though only what nothing references; it does not delete by prefix, and it does not
// reclaim the data of a network deleted through an org cascade (see Organizations.Delete). Forward answers the text "Garbage collection
// started" at once and works in the background. ADMINISTER_SYSTEM; Preview.
func (s *Admin) PurgeFDBGarbage(ctx context.Context) (string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/system/fdb/gc", nil)
	if err != nil {
		return "", nil, err
	}
	var text strings.Builder
	resp, err := s.client.Do(markOperation(req, "Admin.PurgeFDBGarbage"), &text)
	return strings.TrimSpace(text.String()), resp, err
}
