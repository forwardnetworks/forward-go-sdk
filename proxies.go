package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type ProxiesService service

type ProxyServer struct {
	ID                  Identifier `json:"id,omitempty"`
	Name                string     `json:"name"`
	Host                string     `json:"host"`
	Port                int        `json:"port"`
	Protocol            string     `json:"protocol"`
	DisableCertChecking bool       `json:"disableCertChecking,omitempty"`
}

func (s *ProxiesService) List(ctx context.Context, networkID string) ([]ProxyServer, *Response, error) {
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ProxyServer]{Keys: []string{"proxies", "items", "data", "results"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Proxies.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *ProxiesService) Create(ctx context.Context, networkID string, input ProxyServer) (*ProxyServer, *Response, error) {
	if err := validateProxy(input); err != nil {
		return nil, nil, err
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Proxies.Create")
	out := new(ProxyServer)
	response, err := s.client.doRequired(req, out)
	if err == nil && out.ID == "" {
		err = errors.New("forward: proxy create returned no ID")
	}
	return out, response, err
}

func (s *ProxiesService) Update(ctx context.Context, networkID, proxyID string, input ProxyServer) (*ProxyServer, *Response, error) {
	if err := validateProxy(input); err != nil {
		return nil, nil, err
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	proxyID = strings.TrimSpace(proxyID)
	if proxyID == "" {
		return nil, nil, errors.New("forward: proxy ID is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path+"/"+url.PathEscape(proxyID), input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Proxies.Update")
	out := new(ProxyServer)
	response, err := s.client.Do(req, out)
	return out, response, err
}

func (s *ProxiesService) base(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/proxies", nil
}

func validateProxy(input ProxyServer) error {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Host) == "" || input.Port == 0 {
		return errors.New("forward: proxy name, host, and port are required")
	}
	return nil
}
