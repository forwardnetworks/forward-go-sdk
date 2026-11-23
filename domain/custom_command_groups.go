package domain

import (
	"errors"
)

// CustomCommandGroup is a stored group. Type is "CLI" (Commands set) or "SNMP" (OIDs set); a group is one or the other.
// DeviceConnTypes are connection type ids (see ClassicDevices.ConnTypes). Devices are device names or globs the group applies to;
// empty means every device of those types. Disabled keeps the group but stops collecting it. Group names are unique ignoring
// case, and Update and Delete match the stored name exactly.
type CustomCommandGroup struct {
	Name            string   `json:"name"`
	DeviceConnTypes []string `json:"deviceConnTypes,omitempty"`
	Type            string   `json:"type,omitempty"`
	Commands        []string `json:"commands,omitempty"`
	// OIDs are an SNMP group's CustomOID values (names are unique ignoring case within a group, and so are the OIDs).
	OIDs     []CustomOID `json:"oids,omitempty"`
	Devices  []string    `json:"devices,omitempty"`
	Disabled bool        `json:"disabled"`
}

// NewCustomCommandGroup creates a group. Name and DeviceConnTypes are required, and exactly one of Commands and OIDs.
// Disabled nil creates it enabled.
type NewCustomCommandGroup struct {
	Name            string      `json:"name"`
	DeviceConnTypes []string    `json:"deviceConnTypes"`
	Commands        []string    `json:"commands,omitempty"`
	OIDs            []CustomOID `json:"oids,omitempty"`
	Devices         []string    `json:"devices,omitempty"`
	Disabled        *bool       `json:"disabled,omitempty"`
}

// CustomCommandGroupUpdate is a partial update; nil fields are left alone. Devices points at a slice so an empty one ("every
// device of the types") can be sent: use &[]string{}, because a pointer to a nil slice sends null, which Forward reads as "leave
// alone". Commands or OIDs must match the group's existing kind (else 400), and changing DeviceConnTypes re-checks the stored
// commands against the new types; a ":::" sequence needs a collector that supports it here too; Commands and OIDs replace the whole list, and at most one may be set. Name renames the
// group, which must stay unique ignoring case.
type CustomCommandGroupUpdate struct {
	Name            *string     `json:"name,omitempty"`
	DeviceConnTypes []string    `json:"deviceConnTypes,omitempty"`
	Commands        []string    `json:"commands,omitempty"`
	OIDs            []CustomOID `json:"oids,omitempty"`
	Devices         *[]string   `json:"devices,omitempty"`
	Disabled        *bool       `json:"disabled,omitempty"`
}

// ErrCustomCommandGroupNotFound is returned by Get when the network has no group of that name.
var ErrCustomCommandGroupNotFound = errors.New("forward: custom command group not found")

// UnsupportedCustomCommand is a command some of the asked device types do not allow.
type UnsupportedCustomCommand struct {
	Command              string   `json:"command"`
	UnsupportedConnTypes []string `json:"unsupportedConnTypes,omitempty"`
}
