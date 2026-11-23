package domain

import (
	"encoding/json"
	"strings"
)

// OrgProperty is a Forward organization property key. Property names are
// intentionally not enumerated: the available set is version- and
// deployment-dependent.
type OrgProperty string

// PropertyFilter selects effective, configured, or overridden values.
type PropertyFilter string

const (
	PropertyFilterOff          PropertyFilter = "OFF"
	PropertyFilterConfigurable PropertyFilter = "CONFIGURABLE"
	PropertyFilterConfigured   PropertyFilter = "CONFIGURED"
	PropertyFilterOverridden   PropertyFilter = "OVERRIDDEN"
	PropertyFilterNondefault   PropertyFilter = "NONDEFAULT"
)

// PropertyValues retains dynamic bool, number, string, list, and object values
// without lossy conversions.
type PropertyValues map[OrgProperty]json.RawMessage

// Lookup returns the raw value of the named property: an exact key first, then a key that differs only in case (Forward's
// property names are case-insensitive), or nil when there is none.
func (p PropertyValues) Lookup(name OrgProperty) json.RawMessage {
	if raw, ok := p[name]; ok {
		return raw
	}
	for key, raw := range p {
		if strings.EqualFold(string(key), string(name)) {
			return raw
		}
	}
	return nil
}
