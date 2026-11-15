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

// CheckResult reads the rows an NQE check's result holds, page by page, from the result key the check reports (Check.NQEResultKey, from
// Checks.List or Checks.Get): POST /api/nqe/queryResults?snapshotId=&resultKey= (NqeController.getNqeQueryResults, USE_NQE, with the
// check's snapshot; the key is only valid for the snapshot it was computed on, and Forward answers a 400 otherwise). This is the
// full list of a failing NQE check's violating rows, where CheckDiagnosis.Details is only a sample.
//
// Seen live (26.11.0-03, by forward-python-sdk): the answer is Forward's INTERNAL run result, not the published one:
// {"type":{...schema},"diagnostics":[],"executionInfo":{...},"result":{"values":[row...],"totalNumValues":30,...}}, whose cells are typed:
// a scalar is {"value":x[,"metadata":{...}]}, a collection a list of cells, and an enum or one-of {"type":"OS","alternative":{"value":"NXOS"}}.
// CheckResult unwraps the cells (an enum or one-of comes back as its alternative's value, without the type tag), puts the rows in
// Items and totalNumValues in TotalNumItems, so Truncated, RowsAny and DecodeNQE work as on any other result. A body in the published
// shape (items and totalNumItems), as another build may send, decodes as is.
//
// options pages and sorts (Offset, Limit, SortBy, ColumnFilters); nil reads Forward's default first page. The body is the internal
// NqeQueryOptions, which refuses ItemFormat (a 400 naming it), so ItemFormat is never sent from here. Compare TotalNumItems with the
// rows read (NQEResult.Truncated) and page on. Preview: not in the published OpenAPI. Only NQE checks carry a result key; the other
// check types have no equivalent route.
func (s *NQEService) CheckResult(ctx context.Context, snapshotID, resultKey string, options *NQEOptions) (*NQEResult, *Response, error) {
	snapshotID, resultKey = strings.TrimSpace(snapshotID), strings.TrimSpace(resultKey)
	if snapshotID == "" || resultKey == "" {
		return nil, nil, errors.New("forward: a snapshot ID and a check result key are required")
	}
	path := "/api/nqe/queryResults?" + url.Values{"snapshotId": []string{snapshotID}, "resultKey": []string{resultKey}}.Encode()
	var (
		req *http.Request
		err error
	)
	if options == nil {
		req, err = s.client.NewRequest(ctx, http.MethodPost, path, nil)
	} else {
		sent := *options
		sent.ItemFormat = ""
		req, err = s.client.newJSONRequest(ctx, http.MethodPost, path, sent)
	}
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "NQE.CheckResult")
	var raw json.RawMessage
	resp, err := s.client.Do(req, &raw)
	if err != nil {
		return nil, resp, err
	}
	result, err := decodeCheckResult(raw, snapshotID)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}

// decodeCheckResult reads the internal run result (result.values) or, failing that, the published shape.
func decodeCheckResult(raw json.RawMessage, snapshotID string) (*NQEResult, error) {
	var envelope struct {
		Result *struct {
			Values         []json.RawMessage `json:"values"`
			TotalNumValues *int64            `json:"totalNumValues"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("forward: decode NQE check result: %w", err)
	}
	if envelope.Result == nil || envelope.Result.Values == nil && envelope.Result.TotalNumValues == nil {
		result := new(NQEResult)
		if err := json.Unmarshal(raw, result); err != nil {
			return nil, fmt.Errorf("forward: decode NQE check result: %w", err)
		}
		return result, nil
	}
	result := &NQEResult{SnapshotID: Identifier(snapshotID), Items: make([]NQERecord, 0, len(envelope.Result.Values))}
	for i, value := range envelope.Result.Values {
		cell, err := unwrapNQECell(value)
		if err != nil {
			return nil, fmt.Errorf("forward: decode NQE check result row %d: %w", i, err)
		}
		record := NQERecord{}
		if fields, ok := cell.(map[string]any); ok {
			for column, v := range fields {
				encoded, err := json.Marshal(v)
				if err != nil {
					return nil, fmt.Errorf("forward: decode NQE check result row %d: %w", i, err)
				}
				record[column] = encoded
			}
		} else {
			// A row that is not a record of columns (a query that selects one value): keep it under "value".
			encoded, err := json.Marshal(cell)
			if err != nil {
				return nil, fmt.Errorf("forward: decode NQE check result row %d: %w", i, err)
			}
			record["value"] = encoded
		}
		result.Items = append(result.Items, record)
	}
	result.TotalNumItems = int64(len(result.Items))
	if envelope.Result.TotalNumValues != nil {
		result.TotalNumItems = *envelope.Result.TotalNumValues
	}
	return result, nil
}

// unwrapNQECell turns the internal result's typed cells into plain JSON values: {"value":x} is x (its "metadata" dropped), a
// one-of {"type":..,"alternative":c} is c unwrapped, a list is a list of unwrapped cells, and any other object is a record whose
// fields are unwrapped. Numbers keep full precision.
func unwrapNQECell(raw json.RawMessage) (any, error) {
	value, err := decodeExactJSON(raw)
	if err != nil {
		return nil, err
	}
	return unwrapNQEValue(value), nil
}

func unwrapNQEValue(value any) any {
	switch v := value.(type) {
	case []any:
		for i := range v {
			v[i] = unwrapNQEValue(v[i])
		}
		return v
	case map[string]any:
		if alternative, ok := v["alternative"]; ok {
			return unwrapNQEValue(alternative)
		}
		if inner, ok := v["value"]; ok && cellKeysOnly(v) {
			return unwrapNQEValue(inner)
		}
		for key, field := range v {
			v[key] = unwrapNQEValue(field)
		}
		return v
	default:
		return value
	}
}

// cellKeysOnly reports a typed scalar cell: an object with "value" and nothing but the cell's own "metadata" and "type" beside it,
// so a record that merely has a column named "value" is not unwrapped.
func cellKeysOnly(m map[string]any) bool {
	for key := range m {
		if key != "value" && key != "metadata" && key != "type" {
			return false
		}
	}
	return true
}
