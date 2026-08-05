package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type AdminService service

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

func (s *AdminService) ListNetworks(ctx context.Context) ([]AdminNetwork, *Response, error) {
	if err := s.requireService(); err != nil {
		return nil, nil, err
	}
	result := listResponse[AdminNetwork]{Keys: []string{"networks", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/admin/networks", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Admin.ListNetworks")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *AdminService) CreateOrganizationUser(ctx context.Context, orgID string, input AdminUserCreateRequest) (*Response, error) {
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

func (s *AdminService) GrantOrganizationAdmin(ctx context.Context, userID string) (*Response, error) {
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
	if isStatus(err, http.StatusConflict) {
		return response, nil
	}
	return response, err
}

func (s *AdminService) LookupUser(ctx context.Context, idOrUsername string) (*AdminUser, *Response, error) {
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
	out := new(AdminUser)
	response, err := s.client.doRequired(req, out)
	if isStatus(err, http.StatusNotFound) {
		return nil, response, nil
	}
	if err == nil && out.ID == "" {
		err = errors.New("forward: user lookup returned no ID")
	}
	return out, response, err
}

func (s *AdminService) ListUsers(ctx context.Context) ([]AdminUser, *Response, error) {
	if err := s.requireService(); err != nil {
		return nil, nil, err
	}
	result := listResponse[AdminUser]{Keys: []string{"users", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/admin/users", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Admin.ListUsers")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *AdminService) PatchUser(ctx context.Context, userID string, patch AdminUserPatch) (*Response, error) {
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

func (s *AdminService) DeleteUser(ctx context.Context, userID string) (*Response, error) {
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
	if isStatus(err, http.StatusNotFound) {
		return response, nil
	}
	return response, err
}

func (s *AdminService) SetSupportedOrganizations(ctx context.Context, userID string, orgIDs []string) (*Response, error) {
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

func (s *AdminService) AddSupportedOrganization(ctx context.Context, userID, orgID string, expiresDays int) (*Response, error) {
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

func (s *AdminService) requireService() error {
	if s == nil || s.client == nil {
		return errors.New("forward: admin service is nil")
	}
	if s.client.authMode != AuthModeService {
		return errors.New("forward: admin operation requires a service principal")
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
