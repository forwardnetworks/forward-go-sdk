package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// AliasesService reads, creates and deactivates a network's aliases: named sets of hosts, devices,
// interfaces, traffic headers or logical networks that checks and path
// searches refer to by name.
type Aliases service

// List returns the aliases active at a snapshot. GET
// /api/snapshots/{snapshotId}/aliases (getAllAliases, published; AliasController
// on primary 15398425a69 and stable 67e89c87124).
func (s *Aliases) List(ctx context.Context, snapshotID string) ([]ports.Alias, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := snapshotSubPath(snapshotID, "aliases")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Aliases.List")
	result := listResponse[ports.Alias]{Keys: []string{"aliases"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

func aliasPath(snapshotID, name string) (string, error) {
	path, err := snapshotSubPath(snapshotID, "aliases")
	if err != nil {
		return "", err
	}
	if name = strings.TrimSpace(name); name == "" {
		// Never collapse onto the collection route, where a DELETE deactivates every alias.
		return "", errors.New("forward: alias name is required")
	}
	return path + "/" + url.PathEscape(name), nil
}

// Get returns one alias active at a snapshot, with its definition in
// Definition, or (nil, nil) when none is. GET
// /api/snapshots/{snapshotId}/aliases/{name} (getSingleAlias; VIEW_OBJECTS).
// Forward adds the alias's resolved value to the definition, so Definition is
// larger than what Put sent.
func (s *Aliases) Get(ctx context.Context, snapshotID, name string) (*ports.Alias, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := aliasPath(snapshotID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Aliases.Get")
	out := new(ports.Alias)
	resp, err := s.client.doRequired(req, out)
	if ports.IsStatus(err, http.StatusNotFound) {
		return nil, resp, nil
	}
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Put creates an alias, or replaces the definition of the one with the same
// name, and returns it. It is idempotent: putting the same alias again leaves
// the same alias. PUT /api/snapshots/{snapshotId}/aliases/{name}
// (createAlias, published; EDIT_OBJECTS; the snapshot must be past CREATION).
//
// WHEN IT TAKES EFFECT: an alias applies to the snapshot you name and every
// LATER snapshot, including future ones ("Changes to a Snapshot's Aliases
// propagate forward"); snapshots created before it do not see it. So put it on
// the latest snapshot, and it carries on to the ones collected afterwards
// without being put again. Aliases are stored per network, by the snapshot's
// creation time.
//
// Replacing changes only the name and definition fields (Forward's
// createOrReplace). A snapshot forked for Predict needs the PREDICT_UNSANDBOXED org
// property.
func (s *Aliases) Put(ctx context.Context, snapshotID string, alias ports.AliasBuilder) (*ports.Alias, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := alias.Check(); err != nil {
		return nil, nil, err
	}
	path, err := aliasPath(snapshotID, alias.Name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path, alias)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Aliases.Put")
	out := new(ports.Alias)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Deactivate ends an alias at a snapshot: it stops being active at that
// snapshot's creation time (Forward sets its end to one millisecond earlier)
// and so for every later snapshot too, while snapshots created before still
// have it. It returns the alias as it was. Deactivating is permanent for that
// alias: to use the name again Put creates a NEW alias with it. An alias not active at the snapshot
// counts as success and returns (nil, resp, nil). DELETE
// /api/snapshots/{snapshotId}/aliases/{name} (deactivateAlias; EDIT_OBJECTS).
//
// The collection route (DELETE .../aliases, with repeated ?name=) deactivates
// several at once, and with no name EVERY alias active at the snapshot; the
// SDK does not wrap it.
func (s *Aliases) Deactivate(ctx context.Context, snapshotID, name string) (*ports.Alias, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := aliasPath(snapshotID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Aliases.Deactivate")
	out := new(ports.Alias)
	resp, err := s.client.doRequired(req, out)
	if ports.IsGone(err) {
		return nil, resp, nil
	}
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
