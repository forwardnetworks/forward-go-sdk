package forward

import (
	"context"
	"encoding/json"
	"net/http"
)

// AliasesService reads a network's aliases: named sets of hosts, devices,
// interfaces, traffic headers or logical networks that checks and path
// searches refer to by name.
type AliasesService service

// Alias is one alias active at a snapshot (the published Alias schema). Type
// is HOSTS, DEVICES, INTERFACES, HEADERS or LOGICAL_NETWORK. The members
// depend on the type (values, locations, vlanIds, edgeNodes, ...), so
// Definition keeps the whole object as Forward sent it.
type Alias struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	CreatedAt  string          `json:"createdAt,omitempty"`
	CreatorID  Identifier      `json:"creatorId,omitempty"`
	Definition json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the common fields and keeps the full object.
func (a *Alias) UnmarshalJSON(data []byte) error {
	type plain Alias
	var out plain
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*a = Alias(out)
	a.Definition = append(json.RawMessage(nil), data...)
	return nil
}

// List returns the aliases active at a snapshot. GET
// /api/snapshots/{snapshotId}/aliases (getAllAliases, published; AliasController
// on primary 15398425a69 and stable 67e89c87124).
func (s *AliasesService) List(ctx context.Context, snapshotID string) ([]Alias, *Response, error) {
	path, err := snapshotSubPath(snapshotID, "aliases")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Aliases.List")
	result := listResponse[Alias]{Keys: []string{"aliases"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}
