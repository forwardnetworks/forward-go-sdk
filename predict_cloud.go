package forward

// Cloud predict: cloud-object edits on a change set, and the Terraform plan
// import that produces them.
//
// SEPARATE FILE ON PURPOSE. These routes come from Forward's Cloud Editing
// Framework (FWD-59003), which is not in a released build yet, so they stay
// out of the coverage manifest and self-contained here: deleting this file
// removes the capability and leaves the rest compiling. CapabilityCloudPredict
// gates every call so an appserver without the endpoints reports that rather
// than 404ing mid-flow.
//
// The wire contract mirrors firewall predict: cloud objects hang off
// /change-sets/{cs}/devices/{cloudSetup}/cloud-objects/{objectId}, where the
// cloud setup is the collection source that owns the object (its device name
// in Forward) and the object id is the provider's own (rtb-..., sg-...). Edits
// go through the one generic .../edits endpoint (gerrit 227695); the plan
// import and the route-table diff are the additions on top of it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// CloudRoute is one row of a cloud route table as Forward models it: the
// columns of the generated cloud_route_table, so a staged route reads the same
// as a collected one. Every field is always sent -- Forward treats them all as
// present, and an omitted one is a 500 rather than a default -- so a caller
// states a new route fully: Status "active", Propagated "no", Origin "" are the
// values a collected static route carries.
type CloudRoute struct {
	Destination string `json:"destination"`
	Target      string `json:"target"`
	Status      string `json:"status"`
	Propagated  string `json:"propagated"`
	Origin      string `json:"origin"`
}

// InboundRule is one inbound rule of a security group as Forward models it:
// the columns of the generated in_security_group table. A rule has no id;
// Forward keys it by protocol|portRange|source (see InboundRuleKey). Protocol
// "-1" is spelled "all", a port range "from" when both ends match and
// "from,to" otherwise, and a new rule's Action is "allow".
type InboundRule struct {
	Protocol    string `json:"protocol"`
	PortRange   string `json:"portRange"`
	Source      string `json:"source"`
	Description string `json:"description"`
	Action      string `json:"action"`
}

// InboundRuleKey is the identity Forward gives a rule: what it matches.
func InboundRuleKey(protocol, portRange, source string) string {
	return protocol + "|" + portRange + "|" + source
}

// Key is the rule's InboundRuleKey.
func (r InboundRule) Key() string { return InboundRuleKey(r.Protocol, r.PortRange, r.Source) }

// Cloud-object kinds, as the edit endpoint names them.
const (
	CloudObjectRouteTable    = "ROUTE_TABLE"
	CloudObjectSecurityGroup = "SECURITY_GROUP"
)

// CloudObjectImport says what a Terraform plan import staged on one cloud
// object. Type is the object's kind, which says which diff to ask for.
type CloudObjectImport struct {
	ObjectID string `json:"objectId"`
	Type     string `json:"type"`
	Added    int    `json:"added"`
	Modified int    `json:"modified"`
	Removed  int    `json:"removed"`
}

// TerraformPlanImport is the server's account of a plan import: the objects it
// staged changes on, and the resource addresses it had no predictor for.
//
// Unsupported is reported rather than dropped, so a plan touching resources
// Predict cannot model is not silently taken as a smaller change than it is.
type TerraformPlanImport struct {
	Applied     []CloudObjectImport `json:"applied"`
	Unsupported []string            `json:"unsupported"`
}

// RouteDiffEntry is one row of a route-table diff. A is the collected route,
// B the staged one; ADDED rows have no A, DELETED rows no B.
type RouteDiffEntry struct {
	DiffType string      `json:"diffType"` // UNCHANGED | ADDED | MODIFIED | DELETED
	A        *CloudRoute `json:"a,omitempty"`
	B        *CloudRoute `json:"b,omitempty"`
}

// RouteTableDiff is a cloud route table with the draft's changes applied,
// row by row against what was collected.
type RouteTableDiff struct {
	Entries []RouteDiffEntry `json:"entries"`
}

// RuleDiffEntry is one row of a security-group diff, shaped like RouteDiffEntry.
type RuleDiffEntry struct {
	DiffType string       `json:"diffType"`
	A        *InboundRule `json:"a,omitempty"`
	B        *InboundRule `json:"b,omitempty"`
}

// SecurityGroupDiff is a security group's inbound rules with the draft's
// changes applied, rule by rule against what was collected.
type SecurityGroupDiff struct {
	Entries []RuleDiffEntry `json:"entries"`
}

// ImportTerraformPlan stages the cloud-object changes a `terraform show -json`
// plan describes on the change set's draft, for the cloud setup that owns the
// resources the plan touches.
func (s *PredictService) ImportTerraformPlan(
	ctx context.Context,
	networkID string,
	changeSetID string,
	cloudSetup string,
	planJSON []byte,
) (*TerraformPlanImport, *Response, error) {
	if len(planJSON) == 0 {
		return nil, nil, errors.New("forward: terraform plan JSON is required")
	}
	path, err := s.cloudObjectsPath(networkID, changeSetID, cloudSetup)
	if err != nil {
		return nil, nil, err
	}
	path += "?" + url.Values{"action": []string{"importTerraformPlan"}}.Encode()
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, json.RawMessage(planJSON))
	if err != nil {
		return nil, nil, err
	}
	return cloudDo(s, req, new(TerraformPlanImport))
}

// RouteTableDiff returns a route table as the draft would leave it, each row
// marked against the collected table.
func (s *PredictService) RouteTableDiff(
	ctx context.Context,
	networkID string,
	changeSetID string,
	cloudSetup string,
	objectID string,
) (*RouteTableDiff, *Response, error) {
	path, err := s.cloudObjectPath(networkID, changeSetID, cloudSetup, objectID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/diff", nil)
	if err != nil {
		return nil, nil, err
	}
	return cloudDo(s, req, new(RouteTableDiff))
}

// SecurityGroupDiff returns a security group's inbound rules as the draft
// would leave them, each marked against the collected table.
func (s *PredictService) SecurityGroupDiff(
	ctx context.Context,
	networkID string,
	changeSetID string,
	cloudSetup string,
	objectID string,
) (*SecurityGroupDiff, *Response, error) {
	path, err := s.cloudObjectPath(networkID, changeSetID, cloudSetup, objectID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/diff?view=security-group", nil)
	if err != nil {
		return nil, nil, err
	}
	return cloudDo(s, req, new(SecurityGroupDiff))
}

// CloudObjectEditOp is what a cloud-object edit does to one row.
type CloudObjectEditOp string

const (
	CloudObjectEditAdd     CloudObjectEditOp = "ADD"
	CloudObjectEditModify  CloudObjectEditOp = "MODIFY"
	CloudObjectEditRemove  CloudObjectEditOp = "REMOVE"
	CloudObjectEditDiscard CloudObjectEditOp = "DISCARD"
)

// CloudObjectEdit is one edit against one cloud object, typed by object kind
// so a single endpoint serves every kind: Type selects the kind (route tables
// today), Op the operation, and the kind's payload rides in its own field.
//
// RowKey names the base-snapshot row being changed -- a route's destination --
// and is required for MODIFY, REMOVE and DISCARD; Route carries the values for
// ADD and MODIFY. The server refuses a mismatch with a 400.
type CloudObjectEdit struct {
	Type   string            `json:"type"`
	Op     CloudObjectEditOp `json:"op"`
	RowKey string            `json:"rowKey,omitempty"`
	Route  *CloudRoute       `json:"route,omitempty"`
	Rule   *InboundRule      `json:"rule,omitempty"`
}

// EditCloudObject posts one edit to a cloud object on the change set's draft.
// The typed helpers below build the edit; use this directly for a kind they
// do not cover yet.
func (s *PredictService) EditCloudObject(
	ctx context.Context,
	networkID string,
	changeSetID string,
	cloudSetup string,
	objectID string,
	edit CloudObjectEdit,
) (*Response, error) {
	if edit.Type == "" {
		if edit.Rule != nil {
			edit.Type = CloudObjectSecurityGroup
		} else {
			edit.Type = CloudObjectRouteTable
		}
	}
	path, err := s.cloudObjectPath(networkID, changeSetID, cloudSetup, objectID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"/edits", edit)
	if err != nil {
		return nil, err
	}
	_, resp, err := cloudDo[struct{}](s, req, nil)
	return resp, err
}

// AddRoute stages a new route on a cloud route table.
func (s *PredictService) AddRoute(
	ctx context.Context,
	networkID string,
	changeSetID string,
	cloudSetup string,
	objectID string,
	route CloudRoute,
) (*Response, error) {
	return s.EditCloudObject(ctx, networkID, changeSetID, cloudSetup, objectID,
		CloudObjectEdit{Op: CloudObjectEditAdd, Route: &route})
}

// UpdateRoute restates the route currently at destination; the route's own
// Destination may differ, which retargets and renames it in one step.
func (s *PredictService) UpdateRoute(
	ctx context.Context,
	networkID string,
	changeSetID string,
	cloudSetup string,
	objectID string,
	destination string,
	route CloudRoute,
) (*Response, error) {
	return s.EditCloudObject(ctx, networkID, changeSetID, cloudSetup, objectID,
		CloudObjectEdit{Op: CloudObjectEditModify, RowKey: destination, Route: &route})
}

// RemoveRoute stages the removal of the route at destination.
func (s *PredictService) RemoveRoute(
	ctx context.Context,
	networkID string,
	changeSetID string,
	cloudSetup string,
	objectID string,
	destination string,
) (*Response, error) {
	return s.EditCloudObject(ctx, networkID, changeSetID, cloudSetup, objectID,
		CloudObjectEdit{Op: CloudObjectEditRemove, RowKey: destination})
}

// DiscardRoute drops whatever the draft stages for the route at destination,
// leaving the collected row as it was.
func (s *PredictService) DiscardRoute(
	ctx context.Context,
	networkID string,
	changeSetID string,
	cloudSetup string,
	objectID string,
	destination string,
) (*Response, error) {
	return s.EditCloudObject(ctx, networkID, changeSetID, cloudSetup, objectID,
		CloudObjectEdit{Op: CloudObjectEditDiscard, RowKey: destination})
}

// AddInboundRule stages a new inbound rule on a security group.
func (s *PredictService) AddInboundRule(
	ctx context.Context,
	networkID string,
	changeSetID string,
	cloudSetup string,
	objectID string,
	rule InboundRule,
) (*Response, error) {
	return s.EditCloudObject(ctx, networkID, changeSetID, cloudSetup, objectID,
		CloudObjectEdit{Type: CloudObjectSecurityGroup, Op: CloudObjectEditAdd, Rule: &rule})
}

// RemoveInboundRule stages the removal of the rule with the given key.
func (s *PredictService) RemoveInboundRule(
	ctx context.Context,
	networkID string,
	changeSetID string,
	cloudSetup string,
	objectID string,
	key string,
) (*Response, error) {
	return s.EditCloudObject(ctx, networkID, changeSetID, cloudSetup, objectID,
		CloudObjectEdit{Type: CloudObjectSecurityGroup, Op: CloudObjectEditRemove, RowKey: key})
}

// DiscardInboundRule drops whatever the draft stages for the rule with the
// given key.
func (s *PredictService) DiscardInboundRule(
	ctx context.Context,
	networkID string,
	changeSetID string,
	cloudSetup string,
	objectID string,
	key string,
) (*Response, error) {
	return s.EditCloudObject(ctx, networkID, changeSetID, cloudSetup, objectID,
		CloudObjectEdit{Type: CloudObjectSecurityGroup, Op: CloudObjectEditDiscard, RowKey: key})
}

// cloudDo sends a cloud-object request behind the two capability gates and
// records both on success. A nil out sends without decoding a body.
func cloudDo[T any](s *PredictService, req *http.Request, out *T) (*T, *Response, error) {
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
		return nil, nil, err
	}
	if err := s.client.requireCapability(CapabilityCloudPredict); err != nil {
		return nil, nil, err
	}
	var dst any
	if out != nil {
		dst = out
	}
	resp, err := s.client.Do(req, dst)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(CapabilityPredict)
	s.client.observeCapability(CapabilityCloudPredict)
	return out, resp, nil
}

func (s *PredictService) cloudObjectsPath(networkID, changeSetID, cloudSetup string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	path, err := changeSetPath(networkID, changeSetID)
	if err != nil {
		return "", err
	}
	cloudSetup = strings.TrimSpace(cloudSetup)
	if cloudSetup == "" {
		return "", errors.New("forward: cloud setup name is required")
	}
	return fmt.Sprintf("%s/devices/%s/cloud-objects", path, url.PathEscape(cloudSetup)), nil
}

func (s *PredictService) cloudObjectPath(networkID, changeSetID, cloudSetup, objectID string) (string, error) {
	path, err := s.cloudObjectsPath(networkID, changeSetID, cloudSetup)
	if err != nil {
		return "", err
	}
	objectID = strings.TrimSpace(objectID)
	if objectID == "" {
		return "", errors.New("forward: cloud object ID is required")
	}
	return path + "/" + url.PathEscape(objectID), nil
}
