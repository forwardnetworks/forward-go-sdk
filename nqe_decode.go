package forward

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// NQEDecodeOption adjusts DecodeNQE.
type NQEDecodeOption func(*nqeDecodeConfig)

type nqeDecodeConfig struct{ enums bool }

// NQENormalizeEnums turns an enum value NQE writes as "TypeName.VALUE" (AdminStatus.UP, seen live) into its bare name ("UP") before
// decoding, wherever it appears in a row, including inside lists and nested records. It rewrites any string that is exactly an
// identifier, a dot and an upper-case constant name, so a string column that can legitimately hold text of that shape ("Lab.SW1")
// must not be decoded with this option.
func NQENormalizeEnums() NQEDecodeOption { return func(c *nqeDecodeConfig) { c.enums = true } }

var nqeEnumText = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*\.[A-Z][A-Z0-9_]*$`)

// DecodeNQE decodes NQE result rows into T by JSON tag, one value per row: a struct with `json:"Device"` fields, say. Columns the
// struct does not name are ignored and struct fields with no column keep their zero value, as with encoding/json. Integers keep full
// precision (a field of type int64 reads a value above 2^53 exactly, which the float64 route NQERecord's generic readers use would
// not). A row that does not fit T is an error naming its index.
func DecodeNQE[T any](rows []NQERecord, options ...NQEDecodeOption) ([]T, error) {
	var config nqeDecodeConfig
	for _, option := range options {
		option(&config)
	}
	out := make([]T, 0, len(rows))
	for i, row := range rows {
		obj := map[string]json.RawMessage(row)
		if config.enums {
			var err error
			if obj, err = normalizeEnumRow(row); err != nil {
				return nil, fmt.Errorf("forward: NQE row %d: %w", i, err)
			}
		}
		raw, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("forward: NQE row %d: %w", i, err)
		}
		var value T
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("forward: NQE row %d does not fit %T: %w", i, value, err)
		}
		out = append(out, value)
	}
	return out, nil
}

func normalizeEnumRow(row NQERecord) (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(row))
	for column, raw := range row {
		trimmed := strings.TrimSpace(string(raw))
		if !strings.ContainsAny(trimmed[:min(1, len(trimmed))], `"[{`) {
			out[column] = raw
			continue
		}
		value, err := decodeExactJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", column, err)
		}
		rewritten, err := json.Marshal(stripEnumText(value))
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", column, err)
		}
		out[column] = rewritten
	}
	return out, nil
}

func stripEnumText(value any) any {
	switch v := value.(type) {
	case string:
		if nqeEnumText.MatchString(v) {
			return v[strings.IndexByte(v, '.')+1:]
		}
		return v
	case []any:
		for i := range v {
			v[i] = stripEnumText(v[i])
		}
		return v
	case map[string]any:
		for k := range v {
			v[k] = stripEnumText(v[k])
		}
		return v
	default:
		return value
	}
}
