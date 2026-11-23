package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// EffectiveOrganizationValue reads an org's effective value of one property and which level it comes from, so a caller cannot
// mistake the per-property overrides list for effective values. Value is the org's own read; Source comes from two more reads, the
// org's configured rows (GET /api/orgs/{orgId}/config?filter=CONFIGURED) and the global overrides (GET
// /api/global-config?filter=CONFIGURED), following Forward's rule that an org row beats a global override, which beats the built-in
// default (ReadOnlyConfigService.getValue). Needs Forward support permission, as OrganizationValue does. Preview: composed from
// existing reads, untested against a live Forward.
func (s *Properties) EffectiveOrganizationValue(ctx context.Context, orgID string, property domain.OrgProperty) (*domain.EffectiveOrganizationValue, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	value, resp, err := s.OrganizationValue(ctx, orgID, property)
	if err != nil {
		return nil, resp, err
	}
	out := &domain.EffectiveOrganizationValue{Value: value.Lookup(property)}
	own, resp, err := s.Organization(ctx, orgID, domain.PropertyFilterConfigured)
	if err != nil {
		return nil, resp, err
	}
	if own.Lookup(property) != nil {
		out.Source = domain.PropertySourceOrganization
		return out, resp, nil
	}
	global, resp, err := s.Global(ctx, domain.PropertyFilterConfigured)
	if err != nil {
		return nil, resp, err
	}
	out.Source = domain.PropertySourceDefault
	if global.Lookup(property) != nil {
		out.Source = domain.PropertySourceGlobal
	}
	return out, resp, nil
}
