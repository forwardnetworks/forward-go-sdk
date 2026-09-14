package forward

// Cloud predict: staging cloud changes and Terraform plans on a change set.
//
// SEPARATE FILE ON PURPOSE. Cloud predict is not in a released Forward build
// yet, so a public build of this SDK is this repository without this file --
// deleting it removes the whole capability and leaves the rest compiling.
//
// Everything here therefore stays self-contained: no other file references
// these types, and CapabilityCloudPredict gates the calls so an appserver
// without the endpoints reports that rather than 404ing mid-flow.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// CloudChanges is a provider-neutral statement of what a cloud collection
// source should look like after a change.
//
// A caller names a route table and a destination, or a security group and a
// rule, without knowing which provider's state files Predict will edit. The
// server refuses a change a provider has no predictor for, so an AWS-only kind
// stated against Azure comes back as a 400 rather than being ignored.
type CloudChanges struct {
	RouteChanges          []CloudRouteChange          `json:"routeChanges,omitempty"`
	SecurityRuleChanges   []CloudSecurityRuleChange   `json:"securityRuleChanges,omitempty"`
	TgwAssociationChanges []CloudTgwAssociationChange `json:"tgwAssociationChanges,omitempty"`
	TgwPropagationChanges []CloudTgwPropagationChange `json:"tgwPropagationChanges,omitempty"`
	VpnRouteChanges       []CloudVpnRouteChange       `json:"vpnRouteChanges,omitempty"`
}

// CloudRouteChange adds, retargets or removes one route. A destination names
// at most one route in a table, so restating it retargets; an absent Target
// removes it.
type CloudRouteChange struct {
	RouteTableID         string            `json:"routeTableId"`
	DestinationCidrBlock string            `json:"destinationCidrBlock"`
	Target               *CloudRouteTarget `json:"target,omitempty"`
}

// CloudRouteTarget is where a route points. Kind is an AwsRouteTarget name for
// AWS and an AzureNextHopType name for Azure.
type CloudRouteTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// CloudSecurityRuleChange states exactly one of Addition or RemovedRuleID.
type CloudSecurityRuleChange struct {
	SecurityGroupID string             `json:"securityGroupId"`
	Addition        *CloudRuleAddition `json:"addition,omitempty"`
	RemovedRuleID   string             `json:"removedRuleId,omitempty"`
}

// CloudRuleAddition is a security rule stated as its own fields.
//
// Shaped by what AWS needs, where a collected rule carries no id of its own and
// every rule is an unordered allow. Name, Priority and Deny are what a richer
// provider needs on top: Azure names each rule, orders them by priority, and
// lets one deny.
type CloudRuleAddition struct {
	Egress      bool   `json:"egress"`
	Protocol    string `json:"protocol"`
	FromPort    *int64 `json:"fromPort,omitempty"`
	ToPort      *int64 `json:"toPort,omitempty"`
	TargetKind  string `json:"targetKind"`
	TargetID    string `json:"targetId"`
	Description string `json:"description"`
	Name        string `json:"name,omitempty"`
	Priority    *int64 `json:"priority,omitempty"`
	Deny        bool   `json:"deny,omitempty"`
}

// CloudTgwAssociationChange retargets an attachment; an absent RouteTableID
// disassociates it.
type CloudTgwAssociationChange struct {
	AttachmentID string `json:"attachmentId"`
	RouteTableID string `json:"routeTableId,omitempty"`
}

// CloudTgwPropagationChange states whether an attachment's prefixes are learned
// into a route table. The pair is the identity, so Enabled says whether the
// membership exists.
type CloudTgwPropagationChange struct {
	RouteTableID string `json:"routeTableId"`
	AttachmentID string `json:"attachmentId"`
	Enabled      bool   `json:"enabled"`
}

// CloudVpnRouteChange states a destination a VPN connection carries.
//
// Only a statically routed connection states its own destinations; a BGP
// connection learns them from the peer and is refused. What a connection
// carries is what its gateway propagates into the VPC route tables.
type CloudVpnRouteChange struct {
	VpnConnectionID      string `json:"vpnConnectionId"`
	DestinationCidrBlock string `json:"destinationCidrBlock"`
	Present              bool   `json:"present"`
}

// StageCloudChanges replaces the cloud changes staged for a collection source.
//
// SourceName names a collection *source*, not a device: a cloud source models
// many devices, none of which is a device by the source's own name.
func (s *PredictService) StageCloudChanges(
	ctx context.Context,
	networkID string,
	changeSetID string,
	sourceName string,
	changes CloudChanges,
) (*Response, error) {
	return s.stageCloud(ctx, networkID, changeSetID, sourceName, "", changes)
}

// StageTerraformPlan translates `terraform show -json` output into cloud
// changes and stages them.
//
// The server reports what it could not translate rather than dropping it, so a
// plan touching resources Predict has no predictor for is not silently taken
// as a smaller change than it is.
func (s *PredictService) StageTerraformPlan(
	ctx context.Context,
	networkID string,
	changeSetID string,
	sourceName string,
	planJSON []byte,
) (*Response, error) {
	return s.stageCloud(ctx, networkID, changeSetID, sourceName, "fromTerraformPlan", json.RawMessage(planJSON))
}

func (s *PredictService) stageCloud(
	ctx context.Context,
	networkID string,
	changeSetID string,
	sourceName string,
	action string,
	body any,
) (*Response, error) {
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
		return nil, err
	}
	if err := s.client.requireCapability(CapabilityCloudPredict); err != nil {
		return nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	path, err := predictDevicePath(networkID, changeSetID, sourceName)
	if err != nil {
		return nil, err
	}
	path += "/cloud-changes"
	if action != "" {
		path += "?" + url.Values{"action": []string{action}}.Encode()
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	response, err := s.client.Do(req, nil)
	if err == nil {
		s.client.observeCapability(CapabilityPredict)
		s.client.observeCapability(CapabilityCloudPredict)
	}
	return response, err
}

// Commit records the staged draft as a commit on the change set, which is what
// Predict then runs against.
func (s *PredictService) Commit(
	ctx context.Context,
	networkID string,
	changeSetID string,
	note string,
) (*Response, error) {
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
		return nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	path, err := changeSetPath(networkID, changeSetID)
	if err != nil {
		return nil, err
	}
	path += "/commits?" + url.Values{"note": []string{note}}.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	response, err := s.client.Do(req, nil)
	if err == nil {
		s.client.observeCapability(CapabilityPredict)
	}
	return response, err
}
