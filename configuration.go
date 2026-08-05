package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type ConfigurationService service

type DeploymentConfigValue struct {
	Property string
	Value    string
}

func (s *ConfigurationService) GetDeployment(ctx context.Context, property string) (*DeploymentConfigValue, *Response, error) {
	property = strings.TrimSpace(property)
	if property == "" {
		return nil, nil, errors.New("forward: deployment property is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/deployment-config/"+url.PathEscape(property), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Configuration.GetDeployment")
	values := map[string]json.RawMessage{}
	response, err := s.client.doRequired(req, &values)
	if err != nil {
		return nil, response, err
	}
	raw, ok := values[property]
	if !ok {
		return nil, response, fmt.Errorf("forward: deployment response missing property %q", property)
	}
	value, err := scalarConfigValue(raw)
	return &DeploymentConfigValue{Property: property, Value: value}, response, err
}

func (s *ConfigurationService) SetDeployment(ctx context.Context, property, value string) (*DeploymentConfigValue, *Response, error) {
	property = strings.TrimSpace(property)
	if property == "" {
		return nil, nil, errors.New("forward: deployment property is required")
	}
	path := "/api/deployment-config/" + url.PathEscape(property) + "?value=" + url.QueryEscape(strings.TrimSpace(value))
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Configuration.SetDeployment")
	values := map[string]json.RawMessage{}
	response, err := s.client.doRequired(req, &values)
	if err != nil {
		return nil, response, err
	}
	raw, ok := values[property]
	if !ok {
		return nil, response, fmt.Errorf("forward: deployment response missing property %q", property)
	}
	applied, err := scalarConfigValue(raw)
	if err == nil && strings.TrimSpace(applied) != strings.TrimSpace(value) {
		err = fmt.Errorf("forward: deployment property %q applied %q, want %q", property, applied, value)
	}
	return &DeploymentConfigValue{Property: property, Value: applied}, response, err
}

func scalarConfigValue(raw json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case bool:
		return strconv.FormatBool(typed), nil
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("forward: unsupported deployment value %T", value)
	}
}
