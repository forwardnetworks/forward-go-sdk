package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// PropertyKind is the JSON shape of an org property value as the appserver
// serialises it. Forward's config endpoints publish no type metadata -- the
// value itself is the only type information on the wire -- so the kind is
// read from the value, never from a client-side list of names.
type PropertyKind string

const (
	PropertyKindBoolean PropertyKind = "boolean"
	PropertyKindInteger PropertyKind = "integer"
	PropertyKindNumber  PropertyKind = "number"
	PropertyKindString  PropertyKind = "string"
	PropertyKindList    PropertyKind = "list"
	PropertyKindObject  PropertyKind = "object"
	// PropertyKindNull is a property whose value is JSON null: the appserver
	// defines it but holds no value, so its type cannot be observed.
	PropertyKindNull PropertyKind = "null"
)

// PropertyKindOf classifies one raw property value.
func PropertyKindOf(raw json.RawMessage) PropertyKind {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return PropertyKindNull
	}
	switch trimmed[0] {
	case 't', 'f':
		return PropertyKindBoolean
	case '"':
		return PropertyKindString
	case '[':
		return PropertyKindList
	case '{':
		return PropertyKindObject
	}
	if _, err := strconv.ParseInt(string(trimmed), 10, 64); err == nil {
		return PropertyKindInteger
	}
	return PropertyKindNumber
}

// PropertyValueString renders a raw value in the form the Set* methods accept
// as `value`: booleans as true/false, strings unquoted, numbers verbatim, lists
// and objects as compact JSON. JSON null renders as "" -- never as "false";
// "no value" and "false" are different answers.
func PropertyValueString(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			return s
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err == nil {
		return compact.String()
	}
	return string(trimmed)
}

// PropertyDescription is one org property joined across the reads (five, made one after another, not one snapshot: a write between them
// can give an impossible mix such as Layer built-in-default with NonDefault set) that
// together describe it: the org's EFFECTIVE value, whether the org holds an
// explicit override row, and the deployment DEFAULT.
type PropertyDescription struct {
	// Name is the property constant in SCREAMING_CASE. The appserver's JSON keys
	// are lower case (OrgProperty's @JsonValue is name().toLowerCase()); the Set*
	// and Clear* methods accept either spelling.
	Name OrgProperty
	// Kind is observed from the effective value, or from the default when the
	// effective value is null.
	Kind PropertyKind
	// Value is the effective value (override if set, else the default), in
	// PropertyValueString form; Raw is the value as received.
	Value string
	Raw   json.RawMessage
	// Configured reports an explicit override row on this org
	// (filter=CONFIGURED); ConfiguredValue is that row's value.
	Configured      bool
	ConfiguredValue string
	// HasDefault reports that the deployment default was read; Default is it
	// (a global override if set, else the compiled default).
	HasDefault bool
	Default    string
	// Layer says where the effective value comes from, when the extra reads below succeeded (else empty): PropertyLayerOrg (the org has an
	// override row), PropertyLayerGlobal (no org row; a global default override is set) or PropertyLayerCompiled (neither: Forward's built-in
	// default).
	Layer string
	// GlobalOverridden reports a global default override row (Default is then that override, and the built-in default is not exposed).
	GlobalOverridden bool
	// NonDefault reports that the effective value differs from Forward's built-in default.
	NonDefault bool
	// CompiledDefault is Forward's built-in default, known only when no global default override hides it (HasCompiledDefault).
	HasCompiledDefault bool
	CompiledDefault    string
}

// Where a property's effective value comes from (PropertyDescription.Layer). A network can override a few more properties for itself; that
// layer is per network (Properties.NetworkValue), not per org. Forward publishes no Level (ORG or BOTH): a global write to an ORG-level
// property is a 403 (IsGlobalDefaultNotAllowed).
const (
	PropertyLayerOrg      = "org-override"
	PropertyLayerGlobal   = "global-default"
	PropertyLayerCompiled = "built-in-default"
)

// OrganizationPropertyCatalog is every property the appserver reports for one
// org, sorted by name.
type OrganizationPropertyCatalog struct {
	OrgID      string
	Properties []PropertyDescription
	// DefaultsErr is non-nil when the deployment-default read failed. The
	// effective and configured reads are required; the defaults only annotate
	// them, so losing that read leaves HasDefault false everywhere rather than
	// failing the whole description.
	DefaultsErr error
	// LayersErr is non-nil when the reads that tell the layers apart (the global default overrides and the non-default values) failed;
	// Layer, GlobalOverridden, NonDefault and CompiledDefault are then left unset.
	LayersErr error
}

// DescribeOrganization lists EVERY property the appserver defines for orgID,
// with its observed kind, effective value, override state and deployment
// default. The set comes from the appserver (filter=OFF is the whole enum), so
// a property added or retired by an upgrade appears or disappears here with no
// client change. Forward support permission is required.
func (s *PropertiesService) DescribeOrganization(ctx context.Context, orgID string) (*OrganizationPropertyCatalog, *Response, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, nil, errors.New("forward: organization ID is required")
	}
	effective, resp, err := s.Organization(ctx, orgID, PropertyFilterOff)
	if err != nil {
		return nil, resp, err
	}
	configured, resp, err := s.Organization(ctx, orgID, PropertyFilterConfigured)
	if err != nil {
		return nil, resp, err
	}
	defaults, _, defaultsErr := s.Global(ctx, PropertyFilterOff)

	upper := func(values PropertyValues) map[string]json.RawMessage {
		out := make(map[string]json.RawMessage, len(values))
		for key, raw := range values {
			name := strings.ToUpper(strings.TrimSpace(string(key)))
			if name != "" {
				out[name] = raw
			}
		}
		return out
	}
	eff, cfg, def := upper(effective), upper(configured), upper(defaults)
	// Which values are the built-in ones: NONDEFAULT lists effective values that differ from it; the global CONFIGURED filter lists the
	// global default overrides.
	var globalRows, nonDefault map[string]json.RawMessage
	nonDefaultValues, _, nonDefaultErr := s.Organization(ctx, orgID, PropertyFilterNondefault)
	globalValues, _, globalErr := s.Global(ctx, PropertyFilterConfigured)
	layersErr := nonDefaultErr
	if layersErr == nil {
		layersErr = globalErr
	}
	if layersErr == nil {
		globalRows, nonDefault = upper(globalValues), upper(nonDefaultValues)
	}

	catalog := &OrganizationPropertyCatalog{OrgID: orgID, DefaultsErr: defaultsErr, LayersErr: layersErr}
	catalog.Properties = make([]PropertyDescription, 0, len(eff))
	for name, raw := range eff {
		d := PropertyDescription{Name: OrgProperty(name), Kind: PropertyKindOf(raw), Value: PropertyValueString(raw), Raw: raw}
		if row, ok := cfg[name]; ok {
			d.Configured = true
			d.ConfiguredValue = PropertyValueString(row)
		}
		if defaultsErr == nil {
			if dv, ok := def[name]; ok {
				d.HasDefault = true
				d.Default = PropertyValueString(dv)
				if d.Kind == PropertyKindNull {
					d.Kind = PropertyKindOf(dv)
				}
			}
		}
		if layersErr == nil {
			_, d.GlobalOverridden = globalRows[name]
			_, d.NonDefault = nonDefault[name]
			switch {
			case d.Configured:
				d.Layer = PropertyLayerOrg
			case d.GlobalOverridden:
				d.Layer = PropertyLayerGlobal
			default:
				d.Layer = PropertyLayerCompiled
			}
			if d.HasDefault && !d.GlobalOverridden {
				d.HasCompiledDefault, d.CompiledDefault = true, d.Default
			}
		}
		catalog.Properties = append(catalog.Properties, d)
	}
	sort.Slice(catalog.Properties, func(i, j int) bool { return catalog.Properties[i].Name < catalog.Properties[j].Name })
	return catalog, resp, nil
}
