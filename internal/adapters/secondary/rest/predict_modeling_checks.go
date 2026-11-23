package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// Predict model verification and protocol-modeling debug reads (IEntryCheckController, BenchmarkController; master fe0daf63c899).
// Preview: not in the published spec; the protocol-modeling ones are internal routes.

// RunIEntryChecks runs unit packet tests against a snapshot's model (POST /api/snapshots/{snapshotId}/ientrychecks; VIEW_NETWORK_AND_SNAPSHOTS
// and the organization property UNIT_PACKET_TEST; the snapshot must be past creation). Needs at least one check. The results are NOT in
// the order of the checks (Forward groups them by device and table); match each to its check by the check fields they carry. A check
// whose table the device lacks comes back with Status ERROR.
func (s *Predict) RunIEntryChecks(ctx context.Context, snapshotID string, checks []ports.IEntryCheck) ([]ports.IEntryCheckResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	if len(checks) == 0 {
		return nil, nil, errors.New("forward: at least one check is required")
	}
	for _, check := range checks {
		if check.CheckType != ports.IEntryCheckExists && check.CheckType != ports.IEntryCheckNotExists {
			return nil, nil, fmt.Errorf("forward: check %q has check type %q; Forward only knows %q and %q and passes anything else", check.Name, check.CheckType, ports.IEntryCheckExists, ports.IEntryCheckNotExists)
		}
		if check.EntryType != ports.IEntryTypeIPv4 && check.EntryType != ports.IEntryTypeIPv6 {
			return nil, nil, fmt.Errorf("forward: check %q has entry type %q; only %q and %q tables are checked", check.Name, check.EntryType, ports.IEntryTypeIPv4, ports.IEntryTypeIPv6)
		}
		if strings.TrimSpace(check.DeviceName) == "" || len(check.Entry) == 0 {
			return nil, nil, fmt.Errorf("forward: check %q needs a device and an entry", check.Name)
		}
	}
	var out []ports.IEntryCheckResult
	resp, err := s.doJSON(ctx, http.MethodPost, "/api/snapshots/"+url.PathEscape(snapshotID)+"/ientrychecks", checks, &out, "Predict.RunIEntryChecks")
	return out, resp, err
}

// VerifyPredictModeling compares Predict's model of a BASE snapshot with the entries the snapshot collected (POST /api/internal/snapshots/
// {snapshotId}/verify-predict-modeling?mode=base&returnDiffs=; IEntryCheckController at master fe0daf63c899, read from source, not run live).
// With returnDiffs false only the scores come back and diffs is an empty object; the server default is true, which returns the
// missing and extra entries of every device that has a difference and can be very large. Needs MANAGE_PROTOCOL_MODELING (the network ADMIN role; PREDICT licence facet)
// and a snapshot past creation; the route is not gated on the PREDICT_MODELING org property, but with it off no protocol is supported, so the scores should
// come back zero (or the call fails if the prerequisite jobs do; not traced). It is computed on every call, with no stored result to read instead: the master first triggers (or reuses the
// finished result of) the snapshot's protocol-modeling and predictable-routes jobs, which cache derived data for the snapshot, then runs
// the comparison on a search worker with a 90 minute task timeout. Nothing in the comparison itself writes a result, and the route is
// not audited. It can be slow on a large snapshot, so the call waits up to VerifyPredictModelingTimeout instead of the client's usual timeout.
func (s *Predict) VerifyPredictModeling(ctx context.Context, snapshotID string, returnDiffs bool) (*ports.PredictModelVerification, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	query := url.Values{"mode": []string{"base"}}
	if !returnDiffs {
		query.Set("returnDiffs", "false")
	}
	if err := s.client.requireCapability(ports.CapabilityPredict); err != nil {
		return nil, nil, err
	}
	path := "/api/internal/snapshots/" + url.PathEscape(snapshotID) + "/verify-predict-modeling?" + query.Encode()
	req, err := s.client.bodyRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Predict.VerifyPredictModeling")
	out := new(ports.PredictModelVerification)
	resp, err := s.client.doWithTimeout(req, out, false, nil, s.extendTimeout(ports.VerifyPredictModelingTimeout))
	if err != nil {
		return out, resp, err
	}
	s.client.observeCapability(ports.CapabilityPredict)
	return out, resp, nil
}

func (s *Predict) protocolModelingView(ctx context.Context, snapshotID, view string, q ports.ProtocolModelingQuery, operation string) (json.RawMessage, *ports.Response, error) {
	path, err := protocolModelingPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	values, err := protocolModelingQueryValues(q, view)
	if err != nil {
		return nil, nil, err
	}
	var out json.RawMessage
	resp, err := s.doJSON(ctx, http.MethodGet, path+"?"+values.Encode(), nil, &out, operation)
	return out, resp, err
}

// ProtocolSeedRoutes returns the seed route sets containing the prefix, as they were created before propagation, across every protocol
// modeled together (GET .../protocol-modeling?view=seed-routes&prefix=; VIEW_NETWORK_AND_SNAPSHOTS and PREDICT_MODELING). Protocol modeling
// must have run (see ProtocolModelingDone). Forward's source says BGP and OSPF; the answer's shape is Forward's debug output and is
// returned raw. Forward compresses it and the server decompresses it before sending.
func (s *Predict) ProtocolSeedRoutes(ctx context.Context, snapshotID string, q ports.ProtocolModelingQuery) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.protocolModelingView(ctx, snapshotID, "seed-routes", q, "Predict.ProtocolSeedRoutes")
}

// ProtocolPathSelection returns the path-selection candidates for a prefix per protocol (GET .../protocol-modeling?view=path-selection&prefix=),
// including the candidates the search and trace routes prune (the ABSENT ones), for debugging a missing predicted entry; the BGP part also
// has the predecessor links between candidate route sets. Same gates and raw shape as ProtocolSeedRoutes.
func (s *Predict) ProtocolPathSelection(ctx context.Context, snapshotID string, q ports.ProtocolModelingQuery) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.protocolModelingView(ctx, snapshotID, "path-selection", q, "Predict.ProtocolPathSelection")
}

func protocolModelingQueryValues(q ports.ProtocolModelingQuery, view string) (url.Values, error) {
	if strings.TrimSpace(q.Prefix) == "" {
		return nil, errors.New("forward: a prefix is required")
	}
	if strings.TrimSpace(q.VRF) != "" && strings.TrimSpace(q.Device) == "" {
		return nil, errors.New("forward: a VRF needs a device (VRF names are not unique across devices)")
	}
	values := url.Values{"view": []string{view}, "prefix": []string{strings.TrimSpace(q.Prefix)}}
	if device := strings.TrimSpace(q.Device); device != "" {
		values.Set("deviceName", device)
	}
	if vrf := strings.TrimSpace(q.VRF); vrf != "" {
		values.Set("vrfName", vrf)
	}
	return values, nil
}
