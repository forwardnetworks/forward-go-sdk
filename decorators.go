package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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
type DecoratorsService service

// DecoratorEntityType is what a decorator decorates. The join columns it needs
// follow from it: DEVICE (deviceColName), INTERFACE (deviceColName,
// interfaceColName), HOST (ipAddressColName, and optionally hostColName,
// deviceColName, macAddressColName), PATH (none), PATH_DEVICE (hopIndexColName),
// PATH_INTERFACE (hopIndexColName, directionColName). A decorator's type cannot
// be changed once it exists. Only DEVICE, INTERFACE and HOST decorators are
// checked against the query's output (see DecorationInvalidDecorator); the PATH
// types are not.
type DecoratorEntityType string

const (
	DecoratorEntityDevice        DecoratorEntityType = "DEVICE"
	DecoratorEntityInterface     DecoratorEntityType = "INTERFACE"
	DecoratorEntityHost          DecoratorEntityType = "HOST"
	DecoratorEntityPath          DecoratorEntityType = "PATH"
	DecoratorEntityPathDevice    DecoratorEntityType = "PATH_DEVICE"
	DecoratorEntityPathInterface DecoratorEntityType = "PATH_INTERFACE"
)

// Decorator is an entity decorator as Forward lists it. The join columns are
// flat fields; only the ones for Type are set.
type Decorator struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Type is the entity type.
	Type DecoratorEntityType `json:"type"`
	// QueryID is the NQE query ("FQ_..." for a library query) whose rows are attached.
	QueryID string `json:"queryId"`
	// MetadataColNames are the query columns attached to the entity. Empty means every column that is not a join column.
	MetadataColNames  []string `json:"metadataColNames"`
	DeviceColName     string   `json:"deviceColName,omitempty"`
	InterfaceColName  string   `json:"interfaceColName,omitempty"`
	IPAddressColName  string   `json:"ipAddressColName,omitempty"`
	HostColName       string   `json:"hostColName,omitempty"`
	MACAddressColName string   `json:"macAddressColName,omitempty"`
	HopIndexColName   string   `json:"hopIndexColName,omitempty"`
	DirectionColName  string   `json:"directionColName,omitempty"`
	// Enabled decorators are the ones Forward shows on entities.
	Enabled bool `json:"enabled"`
	// SystemManaged decorators are Forward's own ("fwd-" names): they can be enabled or disabled, not edited or deleted.
	SystemManaged bool   `json:"systemManaged"`
	CreatedAt     string `json:"createdAt,omitempty"`
	CreatedBy     string `json:"createdBy,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
	UpdatedBy     string `json:"updatedBy,omitempty"`
}

// DecoratorPut is the body of PATCH /api/decorators/{name}. The call adds the
// decorator when the path name is new and otherwise merges these fields onto the
// existing one: a zero field is left as it is. Type is required and must match an
// existing decorator's type. Creating one needs QueryID, MetadataColNames (pass a
// non-nil empty slice for "all columns") and the join columns for the type, or
// Forward answers 400 (a new decorator takes its name from the body, which Put
// fills from the path name unless Name says otherwise).
type DecoratorPut struct {
	Type DecoratorEntityType `json:"type"`
	// Name renames the decorator to this when it differs from the path name (a name already taken is refused; the status Forward uses
	// is unverified); leave empty to keep the path name. Forward finds an existing decorator by the path name and creates a new one
	// under this body name, so a "fwd-" name here is what Forward refuses as system-managed.
	Name string `json:"name,omitempty"`
	// Description: empty leaves it alone. The SDK cannot send the null that clears it, nor the null that clears a HOST decorator's optional
	// host, device and mac columns.
	Description string `json:"description,omitempty"`
	// QueryID must name a query in the head of the org's NQE library (400 otherwise).
	QueryID string `json:"queryId,omitempty"`
	// MetadataColNames nil leaves the columns alone; a non-nil empty slice means "all columns".
	MetadataColNames *[]string `json:"metadataColNames,omitempty"`
	// Enabled nil leaves it alone; a new decorator is created disabled unless this is true.
	Enabled           *bool  `json:"enabled,omitempty"`
	DeviceColName     string `json:"deviceColName,omitempty"`
	InterfaceColName  string `json:"interfaceColName,omitempty"`
	IPAddressColName  string `json:"ipAddressColName,omitempty"`
	HostColName       string `json:"hostColName,omitempty"`
	MACAddressColName string `json:"macAddressColName,omitempty"`
	HopIndexColName   string `json:"hopIndexColName,omitempty"`
	DirectionColName  string `json:"directionColName,omitempty"`
}

// DecorationStatus is the outcome of one decorator on one entity.
type DecorationStatus string

const (
	DecorationSuccess DecorationStatus = "SUCCESS"
	// DecorationInvalidDecorator means the query's result does not fit the decorator, for DEVICE, INTERFACE and HOST decorators only: a
	// metadata column or a join column is missing from the query's output, or a join column has the wrong type. Required types:
	// DEVICE: deviceColName String. INTERFACE: deviceColName and interfaceColName String. HOST: ipAddressColName IpAddress, and when
	// set hostColName String, deviceColName String, macAddressColName MacAddress. This is where a bad decorator is reported; saving
	// it succeeds. PATH, PATH_DEVICE and PATH_INTERFACE decorators never report it: they use a separate path, where the join columns
	// are not used to match rows (every row the query returns is listed under the path) and a missing metadata column is silently
	// left out of RowType.
	DecorationInvalidDecorator DecorationStatus = "INVALID_DECORATOR"
	// DecorationInvalidQuery means the query does not compile or type-check (or has no committed version).
	DecorationInvalidQuery DecorationStatus = "INVALID_QUERY"
	// DecorationQueryError means the query ran and reported errors.
	DecorationQueryError  DecorationStatus = "QUERY_ERROR"
	DecorationServerError DecorationStatus = "SERVER_ERROR"
	DecorationTimeout     DecorationStatus = "TIMEOUT"
	DecorationCanceled    DecorationStatus = "CANCELED"
)

// EntityDecoration is one decorator's result for one entity. Rows are only
// present on SUCCESS; each is an object keyed by column name, kept as Forward's
// NQE JSON so no value is reshaped. RowType is the NQE record type of the rows.
// EntityID is Forward's spelling of the entity (the catalogue's device name, an
// interface's main name), which may differ in case from what was sent.
type EntityDecoration struct {
	EntityID      string            `json:"entityId"`
	DecoratorName string            `json:"decoratorName"`
	Status        DecorationStatus  `json:"status"`
	RowType       json.RawMessage   `json:"rowType,omitempty"`
	Rows          []json.RawMessage `json:"rows,omitempty"`
}

// DecorateRequest names what to decorate. EntityIDs are device names (DEVICE),
// "<device> <interface>" (INTERFACE), host ids as decimal numbers (HOST), or
// path descriptors (PATH types). Both lists must be non-empty.
type DecorateRequest struct {
	EntityIDs      []string `json:"entityIds"`
	DecoratorNames []string `json:"decoratorNames"`
}

func decoratorPath(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("forward: a decorator name is required")
	}
	return url.PathEscape(name), nil
}

// List returns the org's decorators: Forward's four system-managed ones (named
// "fwd-...", with their enabled flag for this org) followed by the org's own.
// GET /api/decorators.
func (s *DecoratorsService) List(ctx context.Context) ([]Decorator, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/decorators", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Decorators.List")
	result := listResponse[Decorator]{Keys: []string{"decorators", "data", "items", "results"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Get returns the decorator with this name (compared case-insensitively, as
// Forward does), or nil and no error when there is none. Forward has no
// single-decorator read, so this lists and filters.
func (s *DecoratorsService) Get(ctx context.Context, name string) (*Decorator, *Response, error) {
	if _, err := decoratorPath(name); err != nil {
		return nil, nil, err
	}
	all, resp, err := s.List(ctx)
	if err != nil {
		return nil, resp, err
	}
	for i := range all {
		if strings.EqualFold(all[i].Name, name) {
			return &all[i], resp, nil
		}
	}
	return nil, resp, nil
}

// Put adds or merges a user decorator and returns it (PATCH /api/decorators/{name}).
// Forward answers 400 for a system-managed ("fwd-") name, an unknown query id, a
// type that differs from the existing decorator's, or missing required fields on a
// new one, and 403 when the org's licence tier does not permit the query (only
// checked while the decorator is enabled). The columns are NOT checked against the
// query's output here: a mismatch is saved and, for DEVICE, INTERFACE and HOST
// decorators, reported by Decorate as DecorationInvalidDecorator.
func (s *DecoratorsService) Put(ctx context.Context, name string, put DecoratorPut) (*Decorator, *Response, error) {
	path, err := decoratorPath(name)
	if err != nil {
		return nil, nil, err
	}
	switch put.Type {
	case DecoratorEntityDevice, DecoratorEntityInterface, DecoratorEntityHost, DecoratorEntityPath, DecoratorEntityPathDevice, DecoratorEntityPathInterface:
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
	out := new(Decorator)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// Delete removes a user decorator (DELETE /api/decorators/{name}, 204). A
// system-managed name is refused by Forward with 400 (use SetManagedEnabled).
// A 404 for the decorator is success: the desired state, that it is absent,
// already holds. A 404 because this deployment does not serve the route
// (ErrEndpointNotServed) is still an error.
func (s *DecoratorsService) Delete(ctx context.Context, name string) (*Response, error) {
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
	if isGone(err) {
		return resp, nil
	}
	return resp, err
}

// SetManagedEnabled turns one of Forward's system-managed decorators on or off
// for the org (PATCH /api/system-managed-decorators/{name}?enabled=, 204).
// Enabling an unknown name is a 404 and needs the licence tier to permit the
// underlying query (403); disabling an unknown name is accepted (204) and does
// nothing.
func (s *DecoratorsService) SetManagedEnabled(ctx context.Context, name string, enabled bool) (*Response, error) {
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
func (s *DecoratorsService) Decorate(ctx context.Context, networkID, snapshotID string, entityType DecoratorEntityType, req DecorateRequest) ([]EntityDecoration, *Response, error) {
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
	result := listResponse[EntityDecoration]{Keys: []string{"entityDecorations", "decorations", "data", "items"}}
	resp, err := s.client.doWithTimeout(httpReq, &result, false, nil, DecorateTimeout)
	return result.Items, resp, err
}

// DecorateTimeout is the HTTP timeout Decorate uses: Forward gives each decorator
// query up to 5 minutes (EntityDecoratorService TIMEOUT), and a first run on a
// large snapshot can take most of it.
const DecorateTimeout = 6 * time.Minute
