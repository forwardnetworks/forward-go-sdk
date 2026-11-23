package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// DecoratorsService manages NQE entity decorators: a saved NQE query whose
// rows are attached to devices, interfaces, hosts or paths, so that the
// attributes it computes show up next to the entity. A decorator is an org
// object (not per network); Decorate reads what its query attaches to
// entities of one snapshot.
//
// Preview: these routes are not in the published OpenAPI set. Verified against
// web/.../controller/EntityDecoratorController.java (the same five mappings at
// both pinned builds), cv/decorator/{EntityDecorator,EntityDecorators,
// EntityDecoratorService,DecoratorDefinition,EntityDecoration,DecorationStatus}.java,
// web/.../json/decorator/*Patch.java and the search worker's EntityIndexComputer.
// Unverified live: nothing here has been run against a deployment.
type Decorators service

func decoratorPath(name string) (string, error) {
	if err := ports.RequireDecoratorName(name); err != nil {
		return "", err
	}
	return url.PathEscape(name), nil
}

// List returns the org's decorators: Forward's four system-managed ones (named
// "fwd-...", with their enabled flag for this org) followed by the org's own.
// GET /api/decorators.
func (s *Decorators) List(ctx context.Context) ([]ports.Decorator, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/decorators", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Decorators.List")
	result := listResponse[ports.Decorator]{Keys: []string{"decorators", "data", "items", "results"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Put adds or merges a user decorator and returns it (PATCH /api/decorators/{name}).
// Forward answers 400 for a system-managed ("fwd-") name, an unknown query id, a
// type that differs from the existing decorator's, or missing required fields on a
// new one, and 403 when the org's licence tier does not permit the query (only
// checked while the decorator is enabled). The columns are NOT checked against the
// query's output here: a mismatch is saved and, for DEVICE, INTERFACE and HOST
// decorators, reported by Decorate as DecorationInvalidDecorator.
func (s *Decorators) Put(ctx context.Context, name string, put ports.DecoratorPut) (*ports.Decorator, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := decoratorPath(name)
	if err != nil {
		return nil, nil, err
	}
	switch put.Type {
	case ports.DecoratorEntityDevice, ports.DecoratorEntityInterface, ports.DecoratorEntityHost, ports.DecoratorEntityPath, ports.DecoratorEntityPathDevice, ports.DecoratorEntityPathInterface:
	default:
		return nil, nil, errors.New("forward: decorator type must be DEVICE, INTERFACE, HOST, PATH, PATH_DEVICE or PATH_INTERFACE")
	}
	if put.Name == "" {
		put.Name = name
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/decorators/"+path, put)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Decorators.Put")
	out := new(ports.Decorator)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// Delete removes a user decorator (DELETE /api/decorators/{name}, 204). A
// system-managed name is refused by Forward with 400 (use SetManagedEnabled).
// A 404 for the decorator is success: the desired state, that it is absent,
// already holds. A 404 because this deployment does not serve the route
// (ErrEndpointNotServed) is still an error.
func (s *Decorators) Delete(ctx context.Context, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := decoratorPath(name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/decorators/"+path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Decorators.Delete")
	resp, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
		return resp, nil
	}
	return resp, err
}

// SetManagedEnabled turns one of Forward's system-managed decorators on or off
// for the org (PATCH /api/system-managed-decorators/{name}?enabled=, 204).
// Enabling an unknown name is a 404 and needs the licence tier to permit the
// underlying query (403); disabling an unknown name is accepted (204) and does
// nothing.
func (s *Decorators) SetManagedEnabled(ctx context.Context, name string, enabled bool) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := decoratorPath(name)
	if err != nil {
		return nil, err
	}
	q := url.Values{"enabled": {strconv.FormatBool(enabled)}}
	req, err := s.client.NewRequest(ctx, http.MethodPatch, "/api/system-managed-decorators/"+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Decorators.SetManagedEnabled")
	return s.client.Do(req, nil)
}

// Decorate runs the named decorators for the given entities on a snapshot, which is
// how to SEE what a decorator attaches. POST /api/networks/{id}/decorations
// ?type=&snapshotId=. SnapshotID empty uses the network's latest processed snapshot.
//
// The result lists an entity under a decorator only when the decorator has rows for
// it (status SUCCESS, rows non-empty) or when something failed (a failure status,
// no rows); an entity the query has nothing for is simply absent, not an empty
// result. A user decorator runs whether or not it is enabled; a system-managed one
// only when enabled; a name that does not exist, or whose query the licence tier
// denies, is silently absent. Name only decorators of the given entity type:
// Forward does not check that they match ?type=.
//
// Forward answers when its search worker finishes, up to its 5-minute limit, so the
// call uses a DecorateTimeout HTTP timeout. Entities of the wrong shape for the type
// are a 400 ("Invalid path descriptor", an interface not in "<device> <port>" form);
// an unknown device or a non-numeric host id is an error from Forward whose status
// is unverified.
func (s *Decorators) Decorate(ctx context.Context, networkID, snapshotID string, entityType ports.DecoratorEntityType, req ports.DecorateRequest) ([]ports.EntityDecoration, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if len(req.EntityIDs) == 0 || len(req.DecoratorNames) == 0 {
		return nil, nil, errors.New("forward: decorate needs at least one entity id and one decorator name")
	}
	if strings.TrimSpace(string(entityType)) == "" {
		return nil, nil, errors.New("forward: decorate needs an entity type")
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	q := url.Values{"type": {string(entityType)}}
	if snapshotID != "" {
		q.Set("snapshotId", snapshotID)
	}
	httpReq, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/networks/"+url.PathEscape(networkID)+"/decorations?"+q.Encode(), req)
	if err != nil {
		return nil, nil, err
	}
	httpReq = markOperation(httpReq, "Decorators.Decorate")
	result := listResponse[ports.EntityDecoration]{Keys: []string{"entityDecorations", "decorations", "data", "items"}}
	resp, err := s.client.doWithTimeout(httpReq, &result, false, nil, ports.DecorateTimeout)
	return result.Items, resp, err
}
