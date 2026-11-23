package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// Readiness reads the network's performance settings, SNMP credentials and classic devices and reports what Forward's SNMP
// performance polling still needs (see PerformanceReadiness). It makes three requests and needs VIEW_PERFORMANCE_SETTINGS,
// VIEW_DEVICE_CREDENTIALS and permission to read the network's collection sources. Preview: composed from routes that are not in
// the published spec. A failure of any of the three is returned as it is, with no partial answer.
func (s *Performance) Readiness(ctx context.Context, networkID string) (*domain.PerformanceReadiness, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	// Resolved once so all three reads use the same network, including the bound one of a ForNetwork client.
	networkID, err := domain.ResolveNetworkID(networkID, s.session.Network())
	if err != nil {
		return nil, nil, err
	}
	settings, resp, err := s.Settings(ctx, networkID)
	if err != nil {
		return nil, resp, err
	}
	credentials, resp, err := s.credentials.ListSNMPCredentials(ctx, networkID)
	if err != nil {
		return nil, resp, err
	}
	devices, resp, err := s.classicDevices.List(ctx, networkID)
	if err != nil {
		return nil, resp, err
	}
	out := &domain.PerformanceReadiness{
		Enabled:         settings.Enabled,
		IntervalMinutes: settings.Interval(),
		SNMPCredentials: len(credentials),
		Devices:         len(devices),
		Eligible:        []string{},
		Excluded:        []domain.PerformanceExcludedDevice{},
		Blockers:        []string{},
	}
	for _, device := range devices {
		var reasons []string
		// Collection defaults to on when the read does not state it; SNMP collection defaults to off.
		if device.Collect != nil && !*device.Collect {
			reasons = append(reasons, "collection is off")
		}
		if device.EnableSNMPCollection == nil || !*device.EnableSNMPCollection {
			reasons = append(reasons, "SNMP collection is off")
		}
		if len(reasons) == 0 {
			out.Eligible = append(out.Eligible, device.Name)
		} else {
			out.Excluded = append(out.Excluded, domain.PerformanceExcludedDevice{Name: device.Name, Reasons: reasons})
		}
	}
	if !out.Enabled {
		out.Blockers = append(out.Blockers, "performance collection is disabled for the network (PatchSettings)")
	}
	if out.SNMPCredentials == 0 {
		out.Blockers = append(out.Blockers, "the network has no SNMP credential (Credentials.CreateSNMPCredential); Forward refuses to enable collection without one")
	}
	if len(out.Eligible) == 0 {
		out.Blockers = append(out.Blockers, "no device has both collection and SNMP collection on (ClassicDevicePatch.EnableSNMPCollection)")
	}
	return out, resp, nil
}
