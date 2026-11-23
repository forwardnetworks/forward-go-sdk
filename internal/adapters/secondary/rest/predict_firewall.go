package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// Firewall predict: security rules staged on a change set for a classic firewall device (a
// PAN-OS VM-Series, say), in Forward's own rule model rather than vendor CLI. Objects are
// named by scope and name as the device's own object tables list them; "PREDEFINED" holds
// the vendor's built-in applications, services and the "any" user.

// AddSecurityRule stages a new rule in the device's rulebase.
func (s *Predict) AddSecurityRule(
	ctx context.Context, networkID, changeSetID, deviceName, scopeID, rulebaseID string, rule ports.NewSecurityRule,
) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(rule.Definition.Name) == "" {
		return nil, errors.New("forward: security rule name is required")
	}
	path, err := s.rulebasePath(networkID, changeSetID, deviceName, scopeID, rulebaseID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"/security-rules", rule)
	if err != nil {
		return nil, err
	}
	return s.firewallDo(req, nil)
}

// RemoveSecurityRule stages the removal of the rule with the given id in the change set's draft (DELETE .../security-rules/{ruleId}). The
// baseline snapshot is untouched. For a rule from the baseline, DiscardSecurityRuleChange brings it back (as the baseline had it, so an
// earlier edit is lost). For a rule added in this change set, removing it just drops the addition and there is nothing to restore.
// Removing the same rule twice does nothing the second time.
func (s *Predict) RemoveSecurityRule(
	ctx context.Context, networkID, changeSetID, deviceName, scopeID, rulebaseID, ruleID string,
) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := s.rulebasePath(networkID, changeSetID, deviceName, scopeID, rulebaseID)
	if err != nil {
		return nil, err
	}
	ruleID = strings.TrimSpace(ruleID)
	if ruleID == "" {
		return nil, errors.New("forward: security rule id is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path+"/security-rules/"+url.PathEscape(ruleID), nil)
	if err != nil {
		return nil, err
	}
	return s.firewallDo(markOperation(req, "Predict.RemoveSecurityRule"), nil)
}

// SecurityRulesDiff reads the device's rulebases with every rule marked by how the change set alters it.
func (s *Predict) SecurityRulesDiff(
	ctx context.Context, networkID, changeSetID, deviceName string,
) (*ports.SecurityRuleTableDiff, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.changeSetDevicePath(networkID, changeSetID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/security-rules-diff", nil)
	if err != nil {
		return nil, nil, err
	}
	diff := new(ports.SecurityRuleTableDiff)
	resp, err := s.firewallDo(req, diff)
	if err != nil {
		return nil, resp, err
	}
	return diff, resp, nil
}

func (s *Predict) firewallDo(req *http.Request, out any) (*ports.Response, error) {
	if err := s.client.requireCapability(ports.CapabilityPredict); err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req, out)
	if err != nil {
		return resp, err
	}
	s.client.observeCapability(ports.CapabilityPredict)
	return resp, nil
}

// changeSetDevicePath is the firewall-predict device path: unlike the draft commands path, these
// endpoints hang directly off the change set.
func (s *Predict) changeSetDevicePath(networkID, changeSetID, deviceName string) (string, error) {
	path, err := changeSetPath(networkID, changeSetID)
	if err != nil {
		return "", err
	}
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		return "", errors.New("forward: device name is required")
	}
	return path + "/devices/" + url.PathEscape(deviceName), nil
}

func (s *Predict) rulebasePath(networkID, changeSetID, deviceName, scopeID, rulebaseID string) (string, error) {
	path, err := s.changeSetDevicePath(networkID, changeSetID, deviceName)
	if err != nil {
		return "", err
	}
	if scopeID == "" || rulebaseID == "" {
		return "", errors.New("forward: rulebase scope and id are required")
	}
	return fmt.Sprintf("%s/scopes/%s/rulebases/%s", path, url.PathEscape(scopeID), url.PathEscape(rulebaseID)), nil
}
