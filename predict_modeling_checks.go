package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Predict model verification and protocol-modeling debug reads (IEntryCheckController, BenchmarkController; master fe0daf63c899).
// Preview: not in the published spec; the protocol-modeling ones are internal routes.

// IEntryCheck check types and entry types. Forward matches CheckType EXACTLY and in lower case ("exists", "notexists"): any other value is
// silently marked PASS. EntryType names the device table: only the two IP tables are checked ("IPv4", "IPv6"); another table type is a 500.
const (
	IEntryCheckExists    = "exists"
	IEntryCheckNotExists = "notexists"
	IEntryTypeIPv4       = "IPv4"
	IEntryTypeIPv6       = "IPv6"
)

// IEntryCheck is one unit packet test (Forward's IEntryCheck): an entry (a forwarding or routing entry, left raw; it needs a "type" member,
// "IP" or "MPLS", which picks the entry's kind) that must or must not be present on a device in the snapshot's model.
type IEntryCheck struct {
	Name       string          `json:"name"`
	DeviceName string          `json:"deviceName"`
	Entry      json.RawMessage `json:"entry"`
	EntryType  string          `json:"entryType"`
	CheckType  string          `json:"checkType"`
}

// IEntryCheckResult is a check with its verdict. Status is PASS, FAIL or ERROR.
type IEntryCheckResult struct {
	IEntryCheck
	Status      string `json:"status"`
	Description string `json:"description"`
}

// RunIEntryChecks runs unit packet tests against a snapshot's model (POST /api/snapshots/{snapshotId}/ientrychecks; VIEW_NETWORK_AND_SNAPSHOTS
// and the organization property UNIT_PACKET_TEST; the snapshot must be past creation). Needs at least one check. The results are NOT in
// the order of the checks (Forward groups them by device and table); match each to its check by the check fields they carry. A check
// whose table the device lacks comes back with Status ERROR.
func (s *PredictService) RunIEntryChecks(ctx context.Context, snapshotID string, checks []IEntryCheck) ([]IEntryCheckResult, *Response, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	if len(checks) == 0 {
		return nil, nil, errors.New("forward: at least one check is required")
	}
	for _, check := range checks {
		if check.CheckType != IEntryCheckExists && check.CheckType != IEntryCheckNotExists {
			return nil, nil, fmt.Errorf("forward: check %q has check type %q; Forward only knows %q and %q and passes anything else", check.Name, check.CheckType, IEntryCheckExists, IEntryCheckNotExists)
		}
		if check.EntryType != IEntryTypeIPv4 && check.EntryType != IEntryTypeIPv6 {
			return nil, nil, fmt.Errorf("forward: check %q has entry type %q; only %q and %q tables are checked", check.Name, check.EntryType, IEntryTypeIPv4, IEntryTypeIPv6)
		}
		if strings.TrimSpace(check.DeviceName) == "" || len(check.Entry) == 0 {
			return nil, nil, fmt.Errorf("forward: check %q needs a device and an entry", check.Name)
		}
	}
	var out []IEntryCheckResult
	resp, err := s.doJSON(ctx, http.MethodPost, "/api/snapshots/"+url.PathEscape(snapshotID)+"/ientrychecks", checks, &out, "Predict.RunIEntryChecks")
	return out, resp, err
}

// PredictModelVerification is what VerifyPredictModeling answers (SnapshotIEntryComparisonResult): per-device differences between what
// Predict models and what the snapshot collected, an overall score and a score per protocol. The parts are left raw.
type PredictModelVerification struct {
	Diffs            map[string]json.RawMessage `json:"diffs"`
	Score            json.RawMessage            `json:"score"`
	ScoresByProtocol map[string]json.RawMessage `json:"scoresByProtocol"`
}

// VerifyPredictModeling compares Predict's model of a BASE snapshot with the entries the snapshot collected (POST /api/internal/snapshots/
// {snapshotId}/verify-predict-modeling?mode=base&returnDiffs=; MANAGE_PROTOCOL_MODELING; the snapshot must be past creation). With
// returnDiffs false only the scores come back. It can be slow on a large snapshot.
func (s *PredictService) VerifyPredictModeling(ctx context.Context, snapshotID string, returnDiffs bool) (*PredictModelVerification, *Response, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	query := url.Values{"mode": []string{"base"}}
	if !returnDiffs {
		query.Set("returnDiffs", "false")
	}
	out := new(PredictModelVerification)
	path := "/api/internal/snapshots/" + url.PathEscape(snapshotID) + "/verify-predict-modeling?" + query.Encode()
	resp, err := s.doJSON(ctx, http.MethodPost, path, nil, out, "Predict.VerifyPredictModeling")
	return out, resp, err
}

// ProtocolModelingQuery narrows the seed-route and path-selection reads. Prefix is required; Device limits the answer to that device, and
// VRF (which needs a Device) to one of its VRFs.
type ProtocolModelingQuery struct {
	Prefix string
	Device string
	VRF    string
}

func (q ProtocolModelingQuery) values(view string) (url.Values, error) {
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

func (s *PredictService) protocolModelingView(ctx context.Context, snapshotID, view string, q ProtocolModelingQuery, operation string) (json.RawMessage, *Response, error) {
	path, err := protocolModelingPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	values, err := q.values(view)
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
func (s *PredictService) ProtocolSeedRoutes(ctx context.Context, snapshotID string, q ProtocolModelingQuery) (json.RawMessage, *Response, error) {
	return s.protocolModelingView(ctx, snapshotID, "seed-routes", q, "Predict.ProtocolSeedRoutes")
}

// ProtocolPathSelection returns the path-selection candidates for a prefix per protocol (GET .../protocol-modeling?view=path-selection&prefix=),
// including the candidates the search and trace routes prune (the ABSENT ones), for debugging a missing predicted entry; the BGP part also
// has the predecessor links between candidate route sets. Same gates and raw shape as ProtocolSeedRoutes.
func (s *PredictService) ProtocolPathSelection(ctx context.Context, snapshotID string, q ProtocolModelingQuery) (json.RawMessage, *Response, error) {
	return s.protocolModelingView(ctx, snapshotID, "path-selection", q, "Predict.ProtocolPathSelection")
}
