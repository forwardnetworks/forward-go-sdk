package forward

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// listResponse accepts both the bare arrays used by some stable builds and the
// named envelopes used by other builds.
type listResponse[T any] struct {
	Items       []T
	Keys        []string
	AllowSingle bool
	// AllowEmptyObject reads a bare {} as an empty list. Only for envelopes
	// Forward serializes with @JsonInclude(NON_EMPTY), which drop the list key
	// entirely when it is empty; elsewhere an object with no known key stays an
	// error, so an unexpected body never reads as "there are none".
	AllowEmptyObject bool
}

type optionalValue[T any] struct {
	Value   T
	Present bool
}

func (o *optionalValue[T]) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		o.Present = false
		return nil
	}
	o.Present = true
	return json.Unmarshal(data, &o.Value)
}

func (r *listResponse[T]) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		r.Items = nil
		return nil
	}
	if data[0] == '[' {
		return json.Unmarshal(data, &r.Items)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, key := range r.Keys {
		if value, ok := object[key]; ok {
			return json.Unmarshal(value, &r.Items)
		}
	}
	if r.AllowEmptyObject && len(object) == 0 {
		r.Items = nil
		return nil
	}
	if r.AllowSingle {
		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		r.Items = []T{item}
		return nil
	}
	return fmt.Errorf("forward: response has none of the expected list fields %v", r.Keys)
}
