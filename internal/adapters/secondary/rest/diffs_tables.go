package rest

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// The forwarding-state diff families of Forward's DiffController: ARP, MAC, NAT and L3 routing (routes by device and by prefix), the
// bidirectional counts, and the vulnerability counts. ARP, MAC and NAT have the same shape as the ACL diff: a count (Diffs.Count
// with DiffKind("arp"), "mac" or "nat"), the devices that changed with their per-type counts, and one device's entries. L3 routing
// has the device list and entries but no count, and adds a summary and a view by prefix. Vulnerabilities and routing loops are counts. Preview: appserver
// controller, not the published OpenAPI. The routing diff needs VIEW_ROUTE_DIFFS; ARP, MAC, NAT and vulnerabilities their own
// VIEW_*_DIFFS operation; all need a snapshot processed to the creation stage.

// Diffs.Count also answers for the ARP, MAC and NAT families (GET .../arp?count, .../mac?count, .../nat?count: the number of changed
// devices): pass DiffKind("arp"), DiffKind("mac") or DiffKind("nat"). They are not in CountableDiffKinds, which MaterialSummary walks, and the coverage manifest lists these routes under the device-list methods only.

func (s *Diffs) get(ctx context.Context, snapshotA, snapshotB, tail, operation string, out any) (*ports.Response, error) {
	path, err := diffsPath(snapshotA, snapshotB, tail)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.doRequired(markOperation(req, operation), out)
}

func (s *Diffs) deviceSummaries(ctx context.Context, snapshotA, snapshotB, tail, operation string) ([]ports.DeviceDiffSummary, *ports.Response, error) {
	out := listResponse[ports.DeviceDiffSummary]{Keys: []string{"deviceInfos"}}
	resp, err := s.get(ctx, snapshotA, snapshotB, tail, operation, &out)
	if err != nil {
		return nil, resp, err
	}
	return out.Items, resp, nil
}

func (s *Diffs) deviceDiff(ctx context.Context, snapshotA, snapshotB, family, device, operation string, out any) (*ports.Response, error) {
	if device = strings.TrimSpace(device); device == "" {
		return nil, errors.New("forward: device name is required")
	}
	return s.get(ctx, snapshotA, snapshotB, family+"/devices/"+url.PathEscape(device), operation, out)
}

// ARPDevices lists the devices whose ARP tables differ, each with its change counts (GET .../arp, VIEW_ARP_DIFFS).
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) ARPDevices(ctx context.Context, snapshotA, snapshotB string) ([]ports.DeviceDiffSummary, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.deviceSummaries(ctx, snapshotA, snapshotB, "arp", "Diffs.ARPDevices")
}

// MACDevices lists the devices whose MAC tables differ, each with its change counts (GET .../mac, VIEW_MAC_DIFFS).
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) MACDevices(ctx context.Context, snapshotA, snapshotB string) ([]ports.DeviceDiffSummary, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.deviceSummaries(ctx, snapshotA, snapshotB, "mac", "Diffs.MACDevices")
}

// NATDevices lists the devices whose NAT entries differ, each with its change counts (GET .../nat, VIEW_NAT_DIFFS).
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) NATDevices(ctx context.Context, snapshotA, snapshotB string) ([]ports.DeviceDiffSummary, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.deviceSummaries(ctx, snapshotA, snapshotB, "nat", "Diffs.NATDevices")
}

// L3Devices lists the devices whose L3 routing differs, each with its change counts (GET .../l3?view=devices, VIEW_ROUTE_DIFFS). Forward
// computes and caches the routing diff on the first call, which can take a while on a large network.
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) L3Devices(ctx context.Context, snapshotA, snapshotB string) ([]ports.DeviceDiffSummary, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.deviceSummaries(ctx, snapshotA, snapshotB, "l3?view=devices", "Diffs.L3Devices")
}

// DeviceARP lists one device's ARP entries that were added, deleted or modified (GET .../arp/devices/{device}; the caller must be
// allowed to see the device).
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) DeviceARP(ctx context.Context, snapshotA, snapshotB, device string) (*ports.DeviceARPDiff, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.DeviceARPDiff)
	resp, err := s.deviceDiff(ctx, snapshotA, snapshotB, "arp", device, "Diffs.DeviceARP", out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// DeviceMAC lists one device's MAC-table entries that were added, deleted or modified (GET .../mac/devices/{device}; the caller must
// be allowed to see the device).
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) DeviceMAC(ctx context.Context, snapshotA, snapshotB, device string) (*ports.DeviceMACDiff, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.DeviceMACDiff)
	resp, err := s.deviceDiff(ctx, snapshotA, snapshotB, "mac", device, "Diffs.DeviceMAC", out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// DeviceNAT returns one device's NAT diff as Forward sent it (GET .../nat/devices/{device}; the caller must be allowed to see the
// device). The entry JSON comes from a custom serializer that varies by NAT kind, so it is not modelled; the top level is
// {"diffs": [{diffType, a, b}], "stats": {...}}.
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) DeviceNAT(ctx context.Context, snapshotA, snapshotB, device string) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var out json.RawMessage
	resp, err := s.deviceDiff(ctx, snapshotA, snapshotB, "nat", device, "Diffs.DeviceNAT", &out)
	return out, resp, err
}

// DeviceL3 returns one device's L3 routing diff as Forward sent it (GET .../l3/devices/{device}; the caller must be allowed to see
// the device): the routes added, removed and
// changed, with their resolved next hops. The entry JSON comes from a custom serializer, so it is left raw.
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) DeviceL3(ctx context.Context, snapshotA, snapshotB, device string) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var out json.RawMessage
	resp, err := s.deviceDiff(ctx, snapshotA, snapshotB, "l3", device, "Diffs.DeviceL3", &out)
	return out, resp, err
}

// L3Summary returns the network-wide L3 routing diff summary as Forward sent it (GET .../l3/summary, VIEW_ROUTE_DIFFS). Forward
// computes and caches the routing diff on the first call.
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) L3Summary(ctx context.Context, snapshotA, snapshotB string) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var out json.RawMessage
	resp, err := s.get(ctx, snapshotA, snapshotB, "l3/summary", "Diffs.L3Summary", &out)
	return out, resp, err
}

// L3Prefixes returns the routing diff by prefix as Forward sent it, one page at a time (GET .../l3?view=prefixes&offset=&limit=&
// prefixFilter=). Page with Offset and Limit; the body carries the total.
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) L3Prefixes(ctx context.Context, snapshotA, snapshotB string, options ports.L3PrefixOptions) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if options.Offset < 0 || options.Limit < 0 {
		return nil, nil, errors.New("forward: offset and limit cannot be negative")
	}
	query := url.Values{"view": []string{"prefixes"}}
	if options.Offset > 0 {
		query.Set("offset", strconv.Itoa(options.Offset))
	}
	if options.Limit > 0 {
		query.Set("limit", strconv.Itoa(options.Limit))
	}
	if filter := strings.TrimSpace(options.PrefixFilter); filter != "" {
		query.Set("prefixFilter", filter)
	}
	var out json.RawMessage
	resp, err := s.get(ctx, snapshotA, snapshotB, "l3?"+query.Encode(), "Diffs.L3Prefixes", &out)
	return out, resp, err
}

// L3PrefixDetails returns how one prefix's routing changed, on every device that has it, as Forward sent it (GET
// .../l3/prefixes/{prefix}; the prefix is escaped into the path as the UI does).
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) L3PrefixDetails(ctx context.Context, snapshotA, snapshotB, prefix string) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if prefix = strings.TrimSpace(prefix); prefix == "" {
		return nil, nil, errors.New("forward: prefix is required")
	}
	var out json.RawMessage
	resp, err := s.get(ctx, snapshotA, snapshotB, "l3/prefixes/"+url.PathEscape(prefix), "Diffs.L3PrefixDetails", &out)
	return out, resp, err
}

// VulnerabilitiesCounts (VIEW_CVE_DIFFS, held from READ_ONLY up and needing the security licence facet) returns the new CVEs and new internet-exposed vulnerable devices from the first snapshot to the second.
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) VulnerabilitiesCounts(ctx context.Context, snapshotA, snapshotB string) (*ports.VulnerabilitiesDiffSummary, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.VulnerabilitiesDiffSummary)
	resp, err := s.get(ctx, snapshotA, snapshotB, "vulnerabilities/counts", "Diffs.VulnerabilitiesCounts", out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// VulnerabilitiesBidirectional returns the vulnerability change in both directions (GET .../vulnerabilities/counts?view=bidirectional,
// VIEW_CVE_DIFFS; the pinned builds also need the organization property PREDICT_DIFF_INSIGHT_ENHANCEMENTS, otherwise Forward refuses it;
// Forward master retired that property and needs only the permission).
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) VulnerabilitiesBidirectional(ctx context.Context, snapshotA, snapshotB string) (*ports.BidirectionalVulnerabilities, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.BidirectionalVulnerabilities)
	resp, err := s.get(ctx, snapshotA, snapshotB, "vulnerabilities/counts?view=bidirectional", "Diffs.VulnerabilitiesBidirectional", out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// RoutingLoopsBidirectional says whether the second snapshot introduced a routing loop (Warnings) or resolved one (Improvements) (GET
// .../routing-loop/count?view=bidirectional, VIEW_CHECK_DIFFS; the pinned builds also need PREDICT_DIFF_INSIGHT_ENHANCEMENTS, which master retired). Each Count is 0 or 1, not
// a number of loops, and Complete=false with 0 means Forward could not tell, not "none yet". Diffs.Count with DiffRoutingLoop is the
// one-directional count.
//
// Preview: DiffController, not in the published spec.
func (s *Diffs) RoutingLoopsBidirectional(ctx context.Context, snapshotA, snapshotB string) (*ports.BidirectionalRoutingLoops, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.BidirectionalRoutingLoops)
	resp, err := s.get(ctx, snapshotA, snapshotB, "routing-loop/count?view=bidirectional", "Diffs.RoutingLoopsBidirectional", out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
