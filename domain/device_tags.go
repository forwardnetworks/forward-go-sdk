package domain

type DeviceTag struct {
	Name    string   `json:"name"`
	Color   string   `json:"color,omitempty"`
	Devices []string `json:"devices,omitempty"`
}

// DeviceTagPatch changes a tag definition; nil fields are left alone. Name
// renames it everywhere (Forward answers 409 if another tag in the network
// already has the name), and Color is "#rrggbb" in either case.
type DeviceTagPatch struct {
	Name  *string
	Color *string
}
