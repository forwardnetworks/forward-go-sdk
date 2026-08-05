package forward

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Identifier is an API identifier represented as a string in Go. It accepts
// either a JSON string or number because some pre-release Forward APIs use
// numeric IDs while stable APIs generally use strings.
type Identifier string

// String returns the identifier as a string.
func (id Identifier) String() string {
	return string(id)
}

// UnmarshalJSON accepts a JSON string or number.
func (id *Identifier) UnmarshalJSON(data []byte) error {
	if id == nil {
		return fmt.Errorf("forward: unmarshal identifier into nil receiver")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*id = ""
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("forward: decode identifier: %w", err)
		}
		*id = Identifier(value)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("forward: decode identifier: %w", err)
	}
	*id = Identifier(number.String())
	return nil
}
