package domain

import (
	"encoding/json"
	"time"
)

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

// DecorateTimeout is the HTTP timeout Decorate uses: Forward gives each decorator
// query up to 5 minutes (EntityDecoratorService TIMEOUT), and a first run on a
// large snapshot can take most of it.
const DecorateTimeout = 6 * time.Minute
