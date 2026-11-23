package usecases

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"sort"
	"strings"
)

// DescribeOrganization lists EVERY property the appserver defines for orgID,
// with its observed kind, effective value, override state and deployment
// default. The set comes from the appserver (filter=OFF is the whole enum), so
// a property added or retired by an upgrade appears or disappears here with no
// client change. Forward support permission is required.
func (s *Properties) DescribeOrganization(ctx context.Context, orgID string) (*domain.OrganizationPropertyCatalog, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, nil, errors.New("forward: organization ID is required")
	}
	effective, resp, err := s.Organization(ctx, orgID, domain.PropertyFilterOff)
	if err != nil {
		return nil, resp, err
	}
	configured, resp, err := s.Organization(ctx, orgID, domain.PropertyFilterConfigured)
	if err != nil {
		return nil, resp, err
	}
	defaults, _, defaultsErr := s.Global(ctx, domain.PropertyFilterOff)

	upper := func(values domain.PropertyValues) map[string]json.RawMessage {
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
	nonDefaultValues, _, nonDefaultErr := s.Organization(ctx, orgID, domain.PropertyFilterNondefault)
	globalValues, _, globalErr := s.Global(ctx, domain.PropertyFilterConfigured)
	layersErr := nonDefaultErr
	if layersErr == nil {
		layersErr = globalErr
	}
	if layersErr == nil {
		globalRows, nonDefault = upper(globalValues), upper(nonDefaultValues)
	}

	catalog := &domain.OrganizationPropertyCatalog{OrgID: orgID, DefaultsErr: defaultsErr, LayersErr: layersErr}
	catalog.Properties = make([]domain.PropertyDescription, 0, len(eff))
	for name, raw := range eff {
		d := domain.PropertyDescription{Name: domain.OrgProperty(name), Kind: domain.PropertyKindOf(raw), Value: domain.PropertyValueString(raw), Raw: raw}
		if row, ok := cfg[name]; ok {
			d.Configured = true
			d.ConfiguredValue = domain.PropertyValueString(row)
		}
		if defaultsErr == nil {
			if dv, ok := def[name]; ok {
				d.HasDefault = true
				d.Default = domain.PropertyValueString(dv)
				if d.Kind == domain.PropertyKindNull {
					d.Kind = domain.PropertyKindOf(dv)
				}
			}
		}
		if layersErr == nil {
			_, d.GlobalOverridden = globalRows[name]
			_, d.NonDefault = nonDefault[name]
			switch {
			case d.Configured:
				d.Layer = domain.PropertyLayerOrg
			case d.GlobalOverridden:
				d.Layer = domain.PropertyLayerGlobal
			default:
				d.Layer = domain.PropertyLayerCompiled
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
