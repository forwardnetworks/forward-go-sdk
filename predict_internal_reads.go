package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Predict's internal debug reads on a snapshot (BenchmarkController, master fe0daf63c899; read from source, none run live). Preview: internal
// routes, not in the published spec, so their shapes can change without notice and the answers are returned raw. The BGP/OSPF node and rule
// reads need only VIEW_NETWORK_AND_SNAPSHOTS and a snapshot past creation; unlike the protocol-modeling reads they are not gated on the
// PREDICT_MODELING org property.

func internalSnapshotPath(snapshotID string, rest ...string) (string, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return "", errors.New("forward: snapshot ID is required")
	}
	path := "/api/internal/snapshots/" + url.PathEscape(snapshotID)
	for _, element := range rest {
		path += "/" + url.PathEscape(element)
	}
	return path, nil
}

func (s *PredictService) internalGet(ctx context.Context, path string, query url.Values, operation string) (json.RawMessage, *Response, error) {
	return s.internalGetWithin(ctx, path, query, operation, 0)
}

// ProtocolModelingReadTimeout is the HTTP timeout the protocol-modeling blob and partitions reads use when the client's own timeout is
// shorter. Forward queues these as a search task that may wait up to 15 minutes, and builds the whole answer before sending a byte.
const ProtocolModelingReadTimeout = 20 * time.Minute

// extendTimeout is timeout when the client's own limit is shorter, and zero (keep the client's) when it is longer or switched off.
func (s *PredictService) extendTimeout(timeout time.Duration) time.Duration {
	if s.client.httpClient != nil && (s.client.httpClient.Timeout == 0 || s.client.httpClient.Timeout > timeout) {
		return 0
	}
	return timeout
}

func (s *PredictService) internalGetWithin(ctx context.Context, path string, query url.Values, operation string, timeout time.Duration) (json.RawMessage, *Response, error) {
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
		return nil, nil, err
	}
	req, err := s.client.bodyRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	var out json.RawMessage
	resp, err := s.client.doWithTimeout(req, &out, false, nil, s.extendTimeout(timeout))
	if err != nil {
		return out, resp, err
	}
	s.client.observeCapability(CapabilityPredict)
	return out, resp, nil
}

// ProtocolModelingPartitions returns how protocol modeling partitioned the snapshot (GET .../protocol-modeling?view=partitions;
// MANAGE_PROTOCOL_MODELING and PREDICT_MODELING). Protocol modeling must have run. The answer is Forward's debug output, returned raw;
// Forward compresses it and the server decompresses it before sending, as for ProtocolSeedRoutes. When partitioning was not used for the
// snapshot Forward answers 200 with {"error": "..."} instead of an error status, so check the raw answer for an "error" key. The call waits up
// to ProtocolModelingReadTimeout.
func (s *PredictService) ProtocolModelingPartitions(ctx context.Context, snapshotID string) (json.RawMessage, *Response, error) {
	path, err := protocolModelingPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	return s.internalGetWithin(ctx, path, url.Values{"view": []string{"partitions"}}, "Predict.ProtocolModelingPartitions", ProtocolModelingReadTimeout)
}

// DownloadProtocolModeling writes the snapshot's protocol-modeling blob, a ZIP holding one entry named protocol-modeling.blob (GET
// .../protocol-modeling with Accept: application/zip; MANAGE_PROTOCOL_MODELING and PREDICT_MODELING). The blob is Forward's internal
// serialization for its own debugging; the SDK does not decode it. Forward builds the whole answer before sending, so the call waits up to
// ProtocolModelingReadTimeout; if it fails part way, dst may hold a partial ZIP.
func (s *PredictService) DownloadProtocolModeling(ctx context.Context, snapshotID string, dst io.Writer) (*Response, error) {
	if dst == nil {
		return nil, errors.New("forward: destination writer is required")
	}
	path, err := protocolModelingPath(snapshotID)
	if err != nil {
		return nil, err
	}
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/zip")
	req = markOperation(req, "Predict.DownloadProtocolModeling")
	resp, err := s.client.doWithTimeout(req, dst, false, nil, s.extendTimeout(ProtocolModelingReadTimeout))
	if err != nil {
		return resp, err
	}
	s.client.observeCapability(CapabilityPredict)
	return resp, nil
}

// BGPNodeDevices lists the devices that host at least one modeled BGP node, as an object of device id to device name (GET
// .../bgp-nodes?devices; BenchmarkController.getBgpNodeDevices). Raw: the ids are Forward's internal device ids.
func (s *PredictService) BGPNodeDevices(ctx context.Context, snapshotID string) (json.RawMessage, *Response, error) {
	path, err := internalSnapshotPath(snapshotID, "bgp-nodes")
	if err != nil {
		return nil, nil, err
	}
	return s.internalGet(ctx, path, url.Values{"devices": []string{""}}, "Predict.BGPNodeDevices")
}

// OSPFNodeDevices is BGPNodeDevices for OSPF (GET .../ospf-nodes?devices; BenchmarkController.getOspfNodeDevices).
func (s *PredictService) OSPFNodeDevices(ctx context.Context, snapshotID string) (json.RawMessage, *Response, error) {
	path, err := internalSnapshotPath(snapshotID, "ospf-nodes")
	if err != nil {
		return nil, nil, err
	}
	return s.internalGet(ctx, path, url.Values{"devices": []string{""}}, "Predict.OSPFNodeDevices")
}

func deviceVRFPath(snapshotID, device, vrf, leaf string) (string, error) {
	device = strings.TrimSpace(device)
	vrf = strings.TrimSpace(vrf)
	if device == "" || vrf == "" {
		return "", errors.New("forward: a device and a VRF are required")
	}
	return internalSnapshotPath(snapshotID, "devices", device, "vrfs", vrf, leaf)
}

// BGPNode returns one modeled BGP node, a device's VRF, with its neighbors (GET .../devices/{device}/vrfs/{vrf}/bgp-node;
// BenchmarkController.getBgpNode). Forward answers {"bgpNode": null} when the VRF runs no BGP and 404 for an unknown device or VRF.
func (s *PredictService) BGPNode(ctx context.Context, snapshotID, device, vrf string) (json.RawMessage, *Response, error) {
	path, err := deviceVRFPath(snapshotID, device, vrf, "bgp-node")
	if err != nil {
		return nil, nil, err
	}
	return s.internalGet(ctx, path, nil, "Predict.BGPNode")
}

// OSPFNodes returns the modeled OSPF nodes on a device, one per process and area, as {"ospfNodes": [...]} (GET
// .../devices/{device}/ospf-nodes; BenchmarkController.getOspfNodes). A non-empty vrf limits it to that VRF's nodes. A device that runs no
// OSPF gives an empty list, not an error; an unknown device or VRF is a 404.
func (s *PredictService) OSPFNodes(ctx context.Context, snapshotID, device, vrf string) (json.RawMessage, *Response, error) {
	device = strings.TrimSpace(device)
	if device == "" {
		return nil, nil, errors.New("forward: a device is required")
	}
	path, err := internalSnapshotPath(snapshotID, "devices", device, "ospf-nodes")
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{}
	if vrf = strings.TrimSpace(vrf); vrf != "" {
		query.Set("vrfName", vrf)
	}
	return s.internalGet(ctx, path, query, "Predict.OSPFNodes")
}

// BGPRules returns the import and export policy rules of a BGP node's neighbors, per neighbor address family (GET
// .../bgp-rules?deviceName=&vrfName=&neighborAddr=; BenchmarkController.getBgpNodeRulesByName). With neighborAddr empty every neighbor of
// the node is included. A neighborAddr that is not an IP address is refused here, because Forward does not catch the parse failure and
// answers 500 (and would repeat it under a retry policy); a well-formed address that is not a neighbor of the node is a 400, and a VRF
// without a BGP node gives an empty rule list whatever the address.
func (s *PredictService) BGPRules(ctx context.Context, snapshotID, device, vrf, neighborAddr string) (json.RawMessage, *Response, error) {
	device = strings.TrimSpace(device)
	vrf = strings.TrimSpace(vrf)
	if device == "" || vrf == "" {
		return nil, nil, errors.New("forward: a device and a VRF are required")
	}
	path, err := internalSnapshotPath(snapshotID, "bgp-rules")
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"deviceName": []string{device}, "vrfName": []string{vrf}}
	if neighborAddr = strings.TrimSpace(neighborAddr); neighborAddr != "" {
		if _, err := netip.ParseAddr(neighborAddr); err != nil {
			return nil, nil, fmt.Errorf("forward: neighbor address %q is not an IP address", neighborAddr)
		}
		query.Set("neighborAddr", neighborAddr)
	}
	return s.internalGet(ctx, path, query, "Predict.BGPRules")
}

// ChangeSetSubsetOptions tunes ChangeSetSubset. Nil fields leave Forward's defaults (3 iterations, a threshold of 200 devices).
type ChangeSetSubsetOptions struct {
	NumIterations *int
	Threshold     *int
}

// ChangeSetSubset returns the names, sorted, of the devices in the route-protocol blast radius of changedDevices: at most NumIterations
// expansion rounds, stopping once the subset reaches Threshold devices, plus the L2 switches OSPF adjacencies between subset devices cross
// (GET .../rp-nodes?view=changeSetSubset&changedDevices=; BenchmarkController.getChangeSetSubset). The answer is {"names": [...]}. A device
// that is not in the snapshot, a negative NumIterations or a Threshold below 1 is a 400, and the SDK refuses the numbers before sending.
// With OSPF nodes present but no stored L2 domain for the snapshot Forward answers 404.
func (s *PredictService) ChangeSetSubset(ctx context.Context, snapshotID string, changedDevices []string, options ChangeSetSubsetOptions) ([]string, *Response, error) {
	var devices []string
	for _, device := range changedDevices {
		if device = strings.TrimSpace(device); device != "" {
			devices = append(devices, device)
		}
	}
	if len(devices) == 0 {
		return nil, nil, errors.New("forward: at least one changed device is required")
	}
	if options.NumIterations != nil && *options.NumIterations < 0 {
		return nil, nil, errors.New("forward: NumIterations must not be negative")
	}
	if options.Threshold != nil && *options.Threshold < 1 {
		return nil, nil, errors.New("forward: Threshold must be positive")
	}
	path, err := internalSnapshotPath(snapshotID, "rp-nodes")
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"view": []string{"changeSetSubset"}, "changedDevices": devices}
	if options.NumIterations != nil {
		query.Set("numIterations", strconv.Itoa(*options.NumIterations))
	}
	if options.Threshold != nil {
		query.Set("threshold", strconv.Itoa(*options.Threshold))
	}
	raw, resp, err := s.internalGet(ctx, path, query, "Predict.ChangeSetSubset")
	if err != nil {
		return nil, resp, err
	}
	var out struct {
		Names []string `json:"names"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, resp, err
	}
	return out.Names, resp, nil
}

// ConfigNodeDevices lists the devices that host at least one modeled config node (the routes derived from configuration, static or direct),
// as an object of device id to device name (GET .../config-nodes?devices; BenchmarkController.getConfigNodeDevices, Forward master after
// 26.10; not mapped at the 26.10 builds, so those answer 404 "No endpoint" and ErrEndpointNotServed).
func (s *PredictService) ConfigNodeDevices(ctx context.Context, snapshotID string) (json.RawMessage, *Response, error) {
	path, err := internalSnapshotPath(snapshotID, "config-nodes")
	if err != nil {
		return nil, nil, err
	}
	return s.internalGet(ctx, path, url.Values{"devices": []string{""}}, "Predict.ConfigNodeDevices")
}

// ConfigNodes returns the modeled config nodes on a device, one per VRF with direct, local or static routes, as {"configNodes": [...]} (GET
// .../devices/{device}/config-nodes; BenchmarkController.getConfigNodes). A non-empty vrf limits it to that VRF's node. A device without any
// gives an empty list; an unknown device or VRF is a 404.
func (s *PredictService) ConfigNodes(ctx context.Context, snapshotID, device, vrf string) (json.RawMessage, *Response, error) {
	device = strings.TrimSpace(device)
	if device == "" {
		return nil, nil, errors.New("forward: a device is required")
	}
	path, err := internalSnapshotPath(snapshotID, "devices", device, "config-nodes")
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{}
	if vrf = strings.TrimSpace(vrf); vrf != "" {
		query.Set("vrfName", vrf)
	}
	return s.internalGet(ctx, path, query, "Predict.ConfigNodes")
}
