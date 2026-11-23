package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

type Banners service

func (s *Banners) List(ctx context.Context) ([]ports.CustomBanner, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.CustomBanner]{Keys: []string{"banners"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/custom-banners", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Banners.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *Banners) Create(ctx context.Context, input ports.CustomBannerRequest) (*ports.CustomBanner, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := validateBanner(input); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/custom-banners", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Banners.Create")
	out := new(ports.CustomBanner)
	response, err := s.client.Do(req, out)
	if err == nil && strings.TrimSpace(out.Message) == "" {
		return nil, response, nil
	}
	return out, response, err
}

func (s *Banners) Replace(ctx context.Context, banner ports.CustomBanner) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if banner.ID == "" {
		return nil, errors.New("forward: banner ID is required")
	}
	input := ports.CustomBannerRequest{Enabled: banner.Enabled, Message: banner.Message, BackgroundColor: banner.BackgroundColor, NetworkIDs: banner.NetworkIDs}
	if err := validateBanner(input); err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, "/api/custom-banners/"+url.PathEscape(banner.ID.String()), banner)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Banners.Replace")
	return s.client.Do(req, nil)
}

func validateBanner(input ports.CustomBannerRequest) error {
	if strings.TrimSpace(input.Message) == "" || strings.TrimSpace(input.BackgroundColor) == "" || len(normalizeStrings(input.NetworkIDs)) == 0 {
		return errors.New("forward: banner message, background color, and network IDs are required")
	}
	return nil
}

// Delete removes a custom banner; one that is already gone counts as success. A 404 that means the route is not
// served (ErrEndpointNotServed) is still an error.
// DELETE /api/custom-banners/{bannerId} (CustomBannerController.deleteBanner).
func (s *Banners) Delete(ctx context.Context, bannerID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if bannerID = strings.TrimSpace(bannerID); bannerID == "" {
		return nil, errors.New("forward: banner id is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/custom-banners/"+url.PathEscape(bannerID), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Banners.Delete")
	response, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
		return response, nil
	}
	return response, err
}
