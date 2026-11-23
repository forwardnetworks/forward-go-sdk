package domain

import (
	"errors"
	"strings"
)

type Location struct {
	ID            Identifier `json:"id"`
	Name          string     `json:"name"`
	Lat           float64    `json:"lat,omitempty"`
	Lng           float64    `json:"lng,omitempty"`
	City          string     `json:"city,omitempty"`
	AdminDivision string     `json:"adminDivision,omitempty"`
	Country       string     `json:"country,omitempty"`
	DeviceGlobs   []string   `json:"deviceGlobs,omitempty"`
}

// LocationCreateRequest is a location to add. ID is optional: absent, Forward assigns a numeric one, and a stable
// ID you choose is what lets an applier find the location again. City is required if AdminDivision or Country is
// given, and Country if City is (display only).
//
// DeviceGlobs is Forward's hidden (unpublished) dynamic assignment: device names and globs ("sjc-*") that put
// matching devices in this location. Forward recomputes the assignment after every location change; a device that
// matches globs of several locations goes to the one with the lowest id.
type LocationCreateRequest struct {
	ID            *string  `json:"id"`
	Name          string   `json:"name"`
	Lat           float64  `json:"lat"`
	Lng           float64  `json:"lng"`
	City          string   `json:"city,omitempty"`
	AdminDivision string   `json:"adminDivision,omitempty"`
	Country       string   `json:"country,omitempty"`
	DeviceGlobs   []string `json:"deviceGlobs,omitempty"`
}

type AtlasPatch map[string]string

type DeviceCluster struct {
	Name    string   `json:"name"`
	Devices []string `json:"devices"`
}

type DeviceClusterPatch struct {
	Name    *string  `json:"name,omitempty"`
	Devices []string `json:"devices,omitempty"`
}

// LocationPatch changes some of a location; nil fields are left alone. DeviceGlobs replaces the whole list when
// non-nil, and an empty non-nil list clears it. ID renames the location's id. Clearing City, AdminDivision or
// Country is not supported here.
type LocationPatch struct {
	ID            *string
	Name          *string
	Lat           *float64
	Lng           *float64
	City          *string
	AdminDivision *string
	Country       *string
	DeviceGlobs   []string
}

func (p LocationPatch) Body() (map[string]any, error) {
	body := map[string]any{}
	set := func(key string, value *string) {
		if value != nil {
			body[key] = *value
		}
	}
	set("id", p.ID)
	set("name", p.Name)
	set("city", p.City)
	set("adminDivision", p.AdminDivision)
	set("country", p.Country)
	if p.Lat != nil {
		body["lat"] = *p.Lat
	}
	if p.Lng != nil {
		body["lng"] = *p.Lng
	}
	if p.DeviceGlobs != nil {
		body["deviceGlobs"] = p.DeviceGlobs
	}
	if len(body) == 0 {
		return nil, errors.New("forward: a location patch must change something")
	}
	if p.Name != nil && strings.TrimSpace(*p.Name) == "" {
		return nil, errors.New("forward: a location cannot be renamed to nothing")
	}
	return body, nil
}

// AtlasLocation is one location in the grouped atlas and the devices it holds. Devices are assigned to it
// directly; AnchoredDevices are there because they are anchored to a physical device in it (virtual contexts
// and wireless access points); DynamicMatchDevices are there because their name matches one of the
// location's DeviceGlobs. A device is in exactly one of the three. Forward sorts each list.
type AtlasLocation struct {
	LocationID          Identifier `json:"locationId"`
	Devices             []string   `json:"devices,omitempty"`
	AnchoredDevices     []string   `json:"anchoredDevices,omitempty"`
	DynamicMatchDevices []string   `json:"dynamicMatchDevices,omitempty"`
}
