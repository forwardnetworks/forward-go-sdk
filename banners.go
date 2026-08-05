package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type BannersService service

type CustomBanner struct {
	ID              Identifier `json:"id"`
	Enabled         bool       `json:"enabled"`
	Message         string     `json:"message"`
	BackgroundColor string     `json:"backgroundColor"`
	NetworkIDs      []string   `json:"networkIds"`
}

type CustomBannerRequest struct {
	Enabled         bool     `json:"enabled"`
	Message         string   `json:"message"`
	BackgroundColor string   `json:"backgroundColor"`
	NetworkIDs      []string `json:"networkIds"`
}

func (s *BannersService) List(ctx context.Context) ([]CustomBanner, *Response, error) {
	result := listResponse[CustomBanner]{Keys: []string{"banners"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/custom-banners", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Banners.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *BannersService) Create(ctx context.Context, input CustomBannerRequest) (*CustomBanner, *Response, error) {
	if err := validateBanner(input); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/custom-banners", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Banners.Create")
	out := new(CustomBanner)
	response, err := s.client.Do(req, out)
	if err == nil && strings.TrimSpace(out.Message) == "" {
		return nil, response, nil
	}
	return out, response, err
}

func (s *BannersService) Replace(ctx context.Context, banner CustomBanner) (*Response, error) {
	if banner.ID == "" {
		return nil, errors.New("forward: banner ID is required")
	}
	input := CustomBannerRequest{Enabled: banner.Enabled, Message: banner.Message, BackgroundColor: banner.BackgroundColor, NetworkIDs: banner.NetworkIDs}
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

func validateBanner(input CustomBannerRequest) error {
	if strings.TrimSpace(input.Message) == "" || strings.TrimSpace(input.BackgroundColor) == "" || len(normalizeStrings(input.NetworkIDs)) == 0 {
		return errors.New("forward: banner message, background color, and network IDs are required")
	}
	return nil
}
