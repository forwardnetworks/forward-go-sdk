package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

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

// PredictModelScore is one precision/recall/F1 score from the Predict model check (PredictModelScore in Forward's client-plus-structs). The
// three are PERCENTAGES from 0 to 100, not fractions: precision = 100 * matching / modeled, recall = 100 * matching / collected, and F1 their
// harmonic mean. Forward answers all three as 0 whenever no entry matched, so 0 means "nothing matched" and cannot be told apart from
// "nothing modeled" or "nothing collected": the entry counts are not in the response.
type PredictModelScore struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
}

// PredictModelVerification is what VerifyPredictModeling answers (SnapshotIEntryComparisonResult): per-device differences between what
// Predict models and what the snapshot collected, an overall score and a score per protocol. The parts are left raw; OverallScore and
// ProtocolScores decode the scores.
type PredictModelVerification struct {
	Diffs            map[string]json.RawMessage `json:"diffs"`
	Score            json.RawMessage            `json:"score"`
	ScoresByProtocol map[string]json.RawMessage `json:"scoresByProtocol"`
}

// OverallScore decodes Score. It reports false when the response carried none.
func (v *PredictModelVerification) OverallScore() (PredictModelScore, bool, error) {
	if v == nil || len(v.Score) == 0 || string(v.Score) == "null" {
		return PredictModelScore{}, false, nil
	}
	var score PredictModelScore
	if err := json.Unmarshal(v.Score, &score); err != nil {
		return PredictModelScore{}, false, fmt.Errorf("forward: decode the overall model score: %w", err)
	}
	return score, true, nil
}

// ProtocolScores decodes ScoresByProtocol, keyed by Forward's ProtocolConfigType name. The keys that can appear follow the org's
// PREDICT_MODEL_* properties: BGP (MPLS entries count as BGP), OSPF, CONFIG, OMP and IS_IS, narrowed per device by vendor.
// A key is present only for a protocol for which some device counted at least one entry (Forward builds the map from those counts and
// keeps zero rows only in its own logs): a missing key means nothing was counted for that protocol, while a key with all zeros means
// entries existed and none matched. A score that does not decode as precision/recall/f1 reads as zeros, as does an absent field.
func (v *PredictModelVerification) ProtocolScores() (map[string]PredictModelScore, error) {
	if v == nil {
		return nil, nil
	}
	out := make(map[string]PredictModelScore, len(v.ScoresByProtocol))
	for protocol, raw := range v.ScoresByProtocol {
		var score PredictModelScore
		if err := json.Unmarshal(raw, &score); err != nil {
			return nil, fmt.Errorf("forward: decode the %s model score: %w", protocol, err)
		}
		out[protocol] = score
	}
	return out, nil
}

// VerifyPredictModelingTimeout is the HTTP timeout VerifyPredictModeling uses when the client's own timeout is shorter (and keeps the
// client's when it is longer or disabled). Forward lets the comparison run for up to 90 minutes; this stops well short of that, so a
// snapshot that needs longer still fails here, and a caller can lower it for one call with a context deadline.
const VerifyPredictModelingTimeout = 30 * time.Minute

// ProtocolModelingQuery narrows the seed-route and path-selection reads. Prefix is required; Device limits the answer to that device, and
// VRF (which needs a Device) to one of its VRFs.
type ProtocolModelingQuery struct {
	Prefix string
	Device string
	VRF    string
}
