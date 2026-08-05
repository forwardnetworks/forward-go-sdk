package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type TopologyService service

type TopologyLink struct {
	SourcePort string `json:"sourcePort"`
	TargetPort string `json:"targetPort"`
}
type TopologyOverrideLink struct {
	Port1 string `json:"port1"`
	Port2 string `json:"port2"`
}
type TopologyOverrides struct {
	Absent  []TopologyOverrideLink `json:"absent"`
	Present []TopologyOverrideLink `json:"present"`
}
type TopologyOverridesEdit struct {
	AbsentAdditions  []TopologyOverrideLink `json:"absentAdditions,omitempty"`
	AbsentRemovals   []TopologyOverrideLink `json:"absentRemovals,omitempty"`
	PresentAdditions []TopologyOverrideLink `json:"presentAdditions,omitempty"`
	PresentRemovals  []TopologyOverrideLink `json:"presentRemovals,omitempty"`
}

func (s *TopologyService) List(ctx context.Context, snapshotID string) ([]TopologyLink, *Response, error) {
	base, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[TopologyLink]{Keys: []string{"links", "items", "results", "data"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, base+"/topology", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Topology.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}
func (s *TopologyService) Overrides(ctx context.Context, snapshotID string) (*TopologyOverrides, *Response, error) {
	base, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, base+"/topology/overrides", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Topology.Overrides")
	out := new(TopologyOverrides)
	response, err := s.client.doRequired(req, out)
	if out.Absent == nil {
		out.Absent = []TopologyOverrideLink{}
	}
	if out.Present == nil {
		out.Present = []TopologyOverrideLink{}
	}
	return out, response, err
}
func (s *TopologyService) EditOverrides(ctx context.Context, snapshotID string, input TopologyOverridesEdit) (*Response, error) {
	base, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, base+"/topology/overrides", input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Topology.EditOverrides")
	return s.client.Do(req, nil)
}
func topologySnapshotPath(snapshotID string) (string, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return "", errors.New("forward: snapshot ID is required")
	}
	return "/api/snapshots/" + url.PathEscape(snapshotID), nil
}
