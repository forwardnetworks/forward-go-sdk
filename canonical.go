package forward

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Canonical forms for drift detection. Each function returns a NEW slice, sorted by a stable key, holding
// only the declared fields that a caller can write back, with set-valued lists sorted and read-only or
// server-generated fields left out. Hash or compare these, never the raw JSON Forward returned: its key
// order and extra fields can differ between builds and after unrelated changes. None of them changes an
// existing call's behaviour; they are pure functions over values you have already read.

func sortedCopy(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// CanonicalLocations returns the locations sorted by ID (plain string order, so "10" sorts before "9"),
// each with its DeviceGlobs sorted.
func CanonicalLocations(locations []Location) []Location {
	out := make([]Location, len(locations))
	for i, location := range locations {
		location.DeviceGlobs = sortedCopy(location.DeviceGlobs)
		out[i] = location
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// CanonicalWanCircuits returns the circuits sorted by name without Source, which Forward reports (how the
// circuit was defined) and never accepts. The two connections keep their order: swapping them would be a
// different declaration.
func CanonicalWanCircuits(circuits []WanCircuit) []WanCircuit {
	out := make([]WanCircuit, len(circuits))
	for i, circuit := range circuits {
		circuit.Source = ""
		out[i] = circuit
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// CanonicalDeviceTags returns the tags sorted by name, each with its devices sorted and its colour in
// lower case ("#00FF00" and "#00ff00" are the same colour).
func CanonicalDeviceTags(tags []DeviceTag) []DeviceTag {
	out := make([]DeviceTag, len(tags))
	for i, tag := range tags {
		tag.Devices = sortedCopy(tag.Devices)
		tag.Color = strings.ToLower(tag.Color)
		out[i] = tag
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// AliasConvertOptions tunes Alias.Builder. IgnoreKeys names extra top-level keys of a Definition that are
// known to be harmless, so a newer Forward's addition does not fail every conversion. Anything else the
// converter does not recognise is an error.
type AliasConvertOptions struct {
	IgnoreKeys []string
}

// aliasReadOnlyKeys are what Forward adds to a definition on read that a write neither needs nor accepts:
// creation metadata, and resolvedValue, which only Aliases.Get adds (AliasAndValue).
var aliasReadOnlyKeys = map[string]bool{"name": true, "type": true, "createdAt": true, "creatorId": true, "resolvedValue": true}

// aliasDefinitionKeys are the keys each alias type owns. On read they are the same names a write uses:
// an interface alias's vlans object is unwrapped into vlanIds and vlanIntfTypes, and header values
// are an object keyed by lower-case category.
var aliasDefinitionKeys = map[string][]string{
	AliasTypeHosts:          {"values", "locations"},
	AliasTypeDevices:        {"values"},
	AliasTypeInterfaces:     {"values", "vlanIds", "vlanIntfTypes", "isExposurePoint"},
	AliasTypeHeaders:        {"values"},
	AliasTypeLogicalNetwork: {"devices", "edgeNodes"},
}

// Builder maps an alias read from Forward (its Definition) onto the AliasBuilder that would put it, so an
// alias read from one network or snapshot can be written to another. Set-valued lists come back sorted.
//
// It never drops a field silently: a key it does not know, a value of the wrong JSON type, an unknown alias
// type, or a Definition that is missing (the Alias was not decoded by this SDK) is an error naming what it
// could not map. Metadata it knows is not part of the definition (name, type, createdAt, creatorId, and
// resolvedValue from Aliases.Get) is ignored on purpose. The result passes AliasBuilder's own checks, so
// Put will not reject it locally.
func (a Alias) Builder(options AliasConvertOptions) (AliasBuilder, error) {
	if a.Name == "" {
		return AliasBuilder{}, fmt.Errorf("forward: alias has no name")
	}
	if len(a.Definition) == 0 {
		return AliasBuilder{}, fmt.Errorf("forward: alias %q has no Definition to convert (it was not read through this SDK)", a.Name)
	}
	keys, ok := aliasDefinitionKeys[a.Type]
	if !ok {
		return AliasBuilder{}, fmt.Errorf("forward: alias %q has type %q, which this SDK cannot convert", a.Name, a.Type)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(a.Definition, &fields); err != nil {
		return AliasBuilder{}, fmt.Errorf("forward: alias %q definition is not a JSON object: %w", a.Name, err)
	}
	known := map[string]bool{}
	for _, key := range keys {
		known[key] = true
	}
	for key := range aliasReadOnlyKeys {
		known[key] = true
	}
	for _, key := range options.IgnoreKeys {
		known[key] = true
	}
	var unknown []string
	for key := range fields {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) != 0 {
		sort.Strings(unknown)
		return AliasBuilder{}, fmt.Errorf("forward: alias %q (%s) has fields this SDK cannot map: %s", a.Name, a.Type, strings.Join(unknown, ", "))
	}

	builder := AliasBuilder{Name: a.Name, Type: a.Type}
	stringList := func(key string) ([]string, error) {
		raw, present := fields[key]
		if !present || string(raw) == "null" {
			return nil, nil
		}
		var list []string
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("forward: alias %q field %s is not a list of strings: %w", a.Name, key, err)
		}
		return sortedCopy(list), nil
	}
	var err error
	switch a.Type {
	case AliasTypeHosts:
		if builder.Values, err = stringList("values"); err == nil {
			builder.Locations, err = stringList("locations")
		}
	case AliasTypeDevices:
		builder.Values, err = stringList("values")
	case AliasTypeInterfaces:
		if builder.Values, err = stringList("values"); err == nil {
			if builder.VLANIDs, err = stringList("vlanIds"); err == nil {
				builder.VLANIntfTypes, err = stringList("vlanIntfTypes")
			}
		}
		if err == nil {
			if raw, present := fields["isExposurePoint"]; present && string(raw) != "null" {
				var exposure bool
				if jsonErr := json.Unmarshal(raw, &exposure); jsonErr != nil {
					err = fmt.Errorf("forward: alias %q field isExposurePoint is not a boolean: %w", a.Name, jsonErr)
				} else if exposure {
					// Forward omits false (NON_DEFAULT), so only true is a declaration.
					builder.IsExposurePoint = Ptr(true)
				}
			}
		}
	case AliasTypeHeaders:
		var headerValues map[string][]string
		if raw, present := fields["values"]; present && string(raw) != "null" {
			if jsonErr := json.Unmarshal(raw, &headerValues); jsonErr != nil {
				err = fmt.Errorf("forward: alias %q header values are not an object of string lists: %w", a.Name, jsonErr)
			}
		}
		if err == nil {
			builder.HeaderValues = map[string][]string{}
			for key, values := range headerValues {
				builder.HeaderValues[key] = sortedCopy(values)
			}
		}
	case AliasTypeLogicalNetwork:
		if builder.Devices, err = stringList("devices"); err == nil {
			builder.EdgeNodes, err = stringList("edgeNodes")
		}
	}
	if err != nil {
		return AliasBuilder{}, err
	}
	if checkErr := builder.check(); checkErr != nil {
		return AliasBuilder{}, fmt.Errorf("forward: alias %q does not convert to a writable alias: %w", a.Name, checkErr)
	}
	return builder, nil
}

// CanonicalAliases converts every alias with Alias.Builder and returns the builders sorted by name. It fails
// on the first alias it cannot map, naming it, rather than returning a partial list.
func CanonicalAliases(aliases []Alias, options AliasConvertOptions) ([]AliasBuilder, error) {
	out := make([]AliasBuilder, 0, len(aliases))
	for _, alias := range aliases {
		builder, err := alias.Builder(options)
		if err != nil {
			return nil, err
		}
		out = append(out, builder)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
