package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"sort"
	"strings"
	"time"
)

// ChangeReportsService builds a ChangeReport by reading Forward. The report's shape and assembly rules are shared with forward-python-sdk
// (testdata/change_report/RULES.md); this file is the Go side of the reads, which follow the "Reads" rules there: one existing read per section,
// sequential, each with its own time budget, and a section that cannot be read becomes an unavailable section with a typed reason, never a failed
// report and never a silent "no change". Preview: composed only from reads this SDK already has; nothing here was run against a live Forward.
type ChangeReportsService service

// ChangeReportFlow is a named path query compared on both snapshots (an opt-in section). Expectation is "valid" (the flow should keep a VALID
// path), "none" (it is expected to lose it, an isolation intent) or "any".
type ChangeReportFlow struct {
	Name        string
	Expectation string
	Query       TracePathQuery
}

// ChangeReportState asks for the state section: the BGP sessions, LLDP adjacencies, interface admin states and ACL entry counts of both
// snapshots (NQE reads). ExpectedRemoved lists identities (for example "bgp_session:core-1|default|192.0.2.9") whose loss is intended.
type ChangeReportState struct {
	LLDP            LLDPOptions
	Read            StateReadOptions
	ExpectedRemoved []string
}

// ChangeReportOptions controls Compare. The zero value reads every default section of a collected or predicted pair.
type ChangeReportOptions struct {
	// ChangeSetID names the Predict change set behind a predicted after snapshot. It is required for the security_rules section.
	ChangeSetID string
	// AfterKind is "predicted" or "collected"; empty reads it from the after snapshot.
	AfterKind string
	// MaxItems caps every list in the report (default 100).
	MaxItems int
	// CreatedAt pins meta.created_at (UTC, whole seconds); zero uses the current time.
	CreatedAt time.Time
	// SectionTimeout bounds each section's reads (default 5 minutes). A section that runs past it is unavailable with reason timeout.
	SectionTimeout time.Duration
	// Flows, when not empty, adds the flows section; State, when not nil, adds the state section.
	Flows []ChangeReportFlow
	State *ChangeReportState
}

const defaultChangeReportSectionTimeout = 5 * time.Minute

// SDKName and SDKVersion identify this SDK in a report's meta.
const changeReportSDKName = "forward-go-sdk"

// changeReportSDKVersion is this module's version as the build that links it records it; "unknown" inside the module's own tests, where the
// build info says "(devel)".
func changeReportSDKVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "github.com/forwardnetworks/forward-go-sdk" && dep.Version != "" && dep.Version != "(devel)" {
				return dep.Version
			}
		}
	}
	return "unknown"
}

type notReadyError struct{ message string }

func (e *notReadyError) Error() string { return e.message }

// Compare reads the sections of a change report for the base and after snapshots of one network and assembles it. It fails only when it cannot
// build the report at all (no snapshot ids, the after snapshot cannot be read to learn its kind, an invalid option, or the caller's context is
// done); every section read that fails is reported inside the report.
func (s *ChangeReportsService) Compare(ctx context.Context, networkID, baseSnapshotID, afterSnapshotID string, options ChangeReportOptions) (*ChangeReport, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	baseSnapshotID, afterSnapshotID = strings.TrimSpace(baseSnapshotID), strings.TrimSpace(afterSnapshotID)
	if baseSnapshotID == "" || afterSnapshotID == "" {
		return nil, errors.New("forward: a change report needs a base and an after snapshot ID")
	}
	timeout := options.SectionTimeout
	if timeout <= 0 {
		timeout = defaultChangeReportSectionTimeout
	}
	afterKind := options.AfterKind
	var afterSnapshot *Snapshot
	if afterKind == "" || afterKind == "predicted" {
		snapshot, _, err := s.client.Snapshots.Get(ctx, networkID, afterSnapshotID)
		if err != nil {
			return nil, fmt.Errorf("forward: read the after snapshot: %w", err)
		}
		afterSnapshot = snapshot
		if afterKind == "" {
			afterKind = "collected"
			if snapshot.Predicted() {
				afterKind = "predicted"
			}
		}
	}
	if afterKind != "predicted" && afterKind != "collected" {
		return nil, fmt.Errorf("forward: AfterKind %q must be predicted or collected", afterKind)
	}
	createdAt := options.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	var changeSetID any
	if options.ChangeSetID != "" {
		changeSetID = options.ChangeSetID
	}
	opts := map[string]any{
		"created_at":   createdAt.UTC().Format("2006-01-02T15:04:05Z"),
		"sdk":          map[string]any{"name": changeReportSDKName, "version": changeReportSDKVersion()},
		"expectations": map[string]any{"state_expected_removed": []any{}},
	}
	if options.MaxItems > 0 {
		opts["max_items"] = options.MaxItems
	}
	if options.State != nil {
		removed := make([]any, 0, len(options.State.ExpectedRemoved))
		for _, id := range options.State.ExpectedRemoved {
			removed = append(removed, id)
		}
		opts["expectations"] = map[string]any{"state_expected_removed": removed}
	}

	r := &changeReader{service: s, networkID: networkID, base: baseSnapshotID, after: afterSnapshotID, timeout: timeout}
	sections := map[string]any{}
	type reading struct {
		name string
		read func(context.Context) (map[string]any, error)
	}
	plan := []reading{
		{"checks", r.checks}, {"connectivity", r.connectivity}, {"routing", r.routing}, {"config", r.config},
		{"devices", r.devices}, {"diff_counts", r.diffCounts}, {"vulnerabilities", r.vulnerabilities},
	}
	if len(options.Flows) != 0 {
		flows := options.Flows
		plan = append(plan, reading{"flows", func(ctx context.Context) (map[string]any, error) { return r.flows(ctx, flows) }})
	}
	if options.State != nil {
		state := *options.State
		plan = append(plan, reading{"state", func(ctx context.Context) (map[string]any, error) { return r.state(ctx, state) }})
	}
	if afterKind == "predicted" {
		snapshot := afterSnapshot
		plan = append(plan,
			reading{"predict", func(ctx context.Context) (map[string]any, error) { return r.predict(ctx, snapshot) }},
			reading{"security_rules", func(ctx context.Context) (map[string]any, error) { return r.securityRules(ctx, options.ChangeSetID) }},
		)
	}
	for _, step := range plan {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sections[step.name] = r.run(ctx, step.read)
	}
	input := map[string]any{
		"meta": map[string]any{
			"base_snapshot_id": baseSnapshotID, "after_snapshot_id": afterSnapshotID, "after_kind": afterKind,
			"change_set_id": changeSetID, "network_id": networkID,
		},
		"options":  opts,
		"sections": sections,
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return AssembleChangeReport(raw)
}

type changeReader struct {
	service   *ChangeReportsService
	networkID string
	base      string
	after     string
	timeout   time.Duration
}

// run reads one section within its budget and turns a failure into an unavailable section with a typed reason.
func (r *changeReader) run(parent context.Context, read func(context.Context) (map[string]any, error)) map[string]any {
	ctx, cancel := context.WithTimeout(parent, r.timeout)
	defer cancel()
	out, err := read(ctx)
	if err != nil {
		if parent.Err() == nil && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			err = fmt.Errorf("%w: the section's %s budget ran out", ErrPollBudget, r.timeout)
		}
		return map[string]any{"status": "unavailable", "reason": map[string]any{"kind": changeReportReasonKind(err), "message": err.Error()}}
	}
	out["status"] = "ok"
	return out
}

// changeReportReasonKind maps a read failure to one of the report's reason kinds.
func changeReportReasonKind(err error) string {
	var notReady *notReadyError
	switch {
	case errors.Is(err, ErrConnectivityDiffPartial), errors.Is(err, ErrPollBudget), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.As(err, &notReady), errors.Is(err, ErrSnapshotNotProcessed):
		return "not_ready"
	case errors.Is(err, ErrFeatureGated):
		return "feature_gated"
	case errors.Is(err, ErrPermissionDenied), IsStatus(err, http.StatusForbidden), IsStatus(err, http.StatusUnauthorized):
		return "forbidden"
	case errors.Is(err, ErrEndpointNotServed):
		return "not_served"
	}
	return "error"
}

func (r *changeReader) checks(ctx context.Context) (map[string]any, error) {
	types := []string{CheckTypeExists, CheckTypeIsolation, CheckTypeReachability, CheckTypeQueryString, CheckTypePredefined, CheckTypeNetworkQuery}
	items := make([]any, 0)
	terminal := map[string]bool{"PASS": true, "FAIL": true, "ERROR": true, "TIMEOUT": true, "UNLICENSED": true, "SKIPPED": true}
	for _, checkType := range types {
		rows, _, err := r.service.client.Diffs.WaitChecks(ctx, r.base, r.after, checkType, PollOptions[[]CheckDiffEntry]{Interval: 2 * time.Second, MaxInterval: 10 * time.Second})
		if errors.Is(err, ErrEndpointNotServed) {
			continue // a build without this check type contributes no rows
		}
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			var id Identifier
			var name string
			var base, after any
			if row.A != nil {
				id, name, base = row.A.ID, row.A.Name, strings.ToUpper(row.A.Status)
			}
			if row.B != nil {
				id, name, after = row.B.ID, row.B.Name, strings.ToUpper(row.B.Status)
			}
			for _, status := range []any{base, after} {
				if status != nil && !terminal[status.(string)] {
					return nil, &notReadyError{fmt.Sprintf("check %s is still %s", id, status)}
				}
			}
			items = append(items, map[string]any{"check_id": string(id), "name": name, "type": checkType, "base_status": base, "after_status": after})
		}
	}
	return map[string]any{"items": items}, nil
}

func (r *changeReader) connectivity(ctx context.Context) (map[string]any, error) {
	summary, _, err := r.service.client.Diffs.WaitForSubnetConnectivity(ctx, r.base, r.after, ConnectivityWaitOptions{Timeout: r.timeout})
	if err != nil {
		return nil, err
	}
	pairs := map[string]any{}
	for _, kind := range []string{ConnectivityDiffAdded, ConnectivityDiffDeleted, ConnectivityDiffModified} {
		counts, _, err := r.service.client.Diffs.ConnectivityDiffLocationPairs(ctx, r.base, r.after, kind, "")
		if err != nil {
			return nil, err
		}
		list := make([]any, 0, len(counts))
		for _, c := range counts {
			list = append(list, map[string]any{"src": c.Src, "dst": c.Dst, "count": c.Count})
		}
		pairs[kind] = list
	}
	return map[string]any{"evaluated_subnet_pairs": summary.EvaluatedSubnetPairs, "total_subnet_pairs": summary.TotalSubnetPairs, "location_pairs": pairs}, nil
}

func (r *changeReader) routing(ctx context.Context) (map[string]any, error) {
	devices, _, err := r.service.client.Diffs.L3Devices(ctx, r.base, r.after)
	if err != nil {
		return nil, err
	}
	items := make([]any, 0)
	for _, d := range devices {
		added, removed, modified := d.Stats["ADDED"], d.Stats["DELETED"], d.Stats["MODIFIED"]
		if added+removed+modified == 0 {
			continue
		}
		items = append(items, map[string]any{"device": d.DeviceName, "added": added, "removed": removed, "modified": modified})
	}
	return map[string]any{"items": items}, nil
}

func (r *changeReader) config(ctx context.Context) (map[string]any, error) {
	devices, _, err := r.service.client.Diffs.Files(ctx, r.base, r.after, "CONFIG")
	if err != nil {
		return nil, err
	}
	items := make([]any, 0)
	for _, d := range devices {
		files := make([]any, 0, len(d.Files))
		for _, f := range d.Files {
			if short, ok := f.DeviceFileName(); ok {
				files = append(files, short)
			} else {
				files = append(files, f.Name)
			}
		}
		items = append(items, map[string]any{"device": d.Name, "files_changed": files})
	}
	return map[string]any{"items": items}, nil
}

func (r *changeReader) devices(ctx context.Context) (map[string]any, error) {
	rows, _, err := r.service.client.Diffs.Devices(ctx, r.base, r.after)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"added": []any{}, "removed": []any{}, "modified": []any{}}
	for _, row := range rows {
		name := ""
		switch {
		case row.After != nil:
			name = row.After.Name
		case row.Before != nil:
			name = row.Before.Name
		}
		key := ""
		switch strings.ToUpper(row.Type) {
		case "ADDED":
			key = "added"
		case "DELETED":
			key = "removed"
		case "MODIFIED":
			key = "modified"
		}
		if key != "" && name != "" {
			out[key] = append(out[key].([]any), name)
		}
	}
	return out, nil
}

func (r *changeReader) diffCounts(ctx context.Context) (map[string]any, error) {
	names := map[DiffKind]string{
		DiffDevices: "devices", DiffInterfaces: "interfaces", DiffTopology: "topology", DiffL2: "l2", DiffACL: "acl",
		DiffCloudObjects: "cloud_objects", DiffCloudACL: "cloud_acl", DiffRoutingLoop: "routing_loop",
	}
	families := map[string]any{}
	for _, kind := range CountableDiffKinds {
		count, _, err := r.service.client.Diffs.Count(ctx, r.base, r.after, kind)
		if errors.Is(err, ErrEndpointNotServed) {
			continue // a family the build does not serve is left out
		}
		if err != nil {
			return nil, err
		}
		families[names[kind]] = map[string]any{"count": count.Count, "complete": count.Complete}
	}
	return map[string]any{"families": families}, nil
}

func (r *changeReader) vulnerabilities(ctx context.Context) (map[string]any, error) {
	both, _, err := r.service.client.Diffs.VulnerabilitiesBidirectional(ctx, r.base, r.after)
	if err != nil {
		return nil, err
	}
	var exposed any
	if both.Warnings.ExposedVulnerableDevicesCount != nil {
		exposed = *both.Warnings.ExposedVulnerableDevicesCount
	}
	return map[string]any{"introduced": both.Warnings.CVECount, "resolved": both.Improvements.CVECount, "exposed_devices": exposed}, nil
}

// state reads the four NQE state families on both snapshots.
func (r *changeReader) state(ctx context.Context, options ChangeReportState) (map[string]any, error) {
	read := func(snapshotID string) ([]any, error) {
		nqe := r.service.client.NQE
		items := make([]any, 0)
		sessions, _, err := nqe.BGPSessions(ctx, r.networkID, snapshotID, options.Read)
		if err != nil {
			return nil, err
		}
		for _, s := range sessions {
			items = append(items, map[string]any{"kind": "bgp_session", "identity": []any{s.Device, s.VRF, s.NeighborIP}, "value": s.SessionState})
		}
		adjacencies, _, err := nqe.LLDPAdjacencies(ctx, r.networkID, snapshotID, LLDPOptions{StateReadOptions: options.Read, Devices: options.LLDP.Devices, SkipInterfaces: options.LLDP.SkipInterfaces})
		if err != nil {
			return nil, err
		}
		for _, a := range adjacencies {
			items = append(items, map[string]any{"kind": "lldp_adjacency", "identity": []any{a.Device, a.LocalIntf, a.RemoteSys, a.RemotePort}, "value": "present"})
		}
		interfaces, _, err := nqe.InterfaceAdmin(ctx, r.networkID, snapshotID, options.Read)
		if err != nil {
			return nil, err
		}
		for _, i := range interfaces {
			items = append(items, map[string]any{"kind": "interface_admin", "identity": []any{i.Device, i.Interface}, "value": i.AdminStatus})
		}
		counts, _, err := nqe.ACLCounts(ctx, r.networkID, snapshotID, options.Read)
		if err != nil {
			return nil, err
		}
		keys := make([]ACLKey, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return lessKey([]string{keys[i].Device, keys[i].AclName}, []string{keys[j].Device, keys[j].AclName})
		})
		for _, k := range keys {
			items = append(items, map[string]any{"kind": "acl_entries", "identity": []any{k.Device, k.AclName}, "value": counts[k]})
		}
		return items, nil
	}
	base, err := read(r.base)
	if err != nil {
		return nil, err
	}
	after, err := read(r.after)
	if err != nil {
		return nil, err
	}
	return map[string]any{"base": base, "after": after}, nil
}

var flowBuckets = map[string]string{
	"VALID": "valid", "DROPPED": "dropped", "BLACKHOLE": "blackhole", "UNREACHABLE": "unreachable", "LOOP": "loop",
	"INADMISSIBLE": "other", "POTENTIAL_LOOP": "other", "IGNORED": "other",
}

// flows reads each named path query on both snapshots. The count of a bucket is the number of distinct device paths in that FlowType group.
func (r *changeReader) flows(ctx context.Context, flows []ChangeReportFlow) (map[string]any, error) {
	items := make([]any, 0, len(flows))
	for _, flow := range flows {
		item := map[string]any{"name": flow.Name, "expectation": flow.Expectation}
		base, baseErr := r.flowCounts(ctx, r.base, flow.Query)
		after, afterErr := r.flowCounts(ctx, r.after, flow.Query)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := firstError(baseErr, afterErr); err != nil {
			item["unavailable"] = map[string]any{"kind": changeReportReasonKind(err), "message": err.Error()}
		} else {
			item["base"], item["after"] = base, after
		}
		items = append(items, item)
	}
	return map[string]any{"items": items}, nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *changeReader) flowCounts(ctx context.Context, snapshotID string, query TracePathQuery) (map[string]any, error) {
	facets, _, err := r.service.client.Trace.FacetsQuery(ctx, r.networkID, snapshotID, query, TraceFacetsOptions{})
	if err != nil {
		return nil, err
	}
	if facets.TimedOut {
		return nil, fmt.Errorf("%w: Forward's path search timed out on snapshot %s", ErrPollBudget, snapshotID)
	}
	if total, ok, _ := facets.Total(); ok && total.Type == "LOWER_BOUND" {
		return nil, fmt.Errorf("%w: Forward capped the flows it examined on snapshot %s (a lower bound, not a count)", ErrPollBudget, snapshotID)
	}
	if len(facets.PathGroups) == 0 && len(facets.PossibleIPLocations) > 0 && string(facets.PossibleIPLocations) != "null" {
		return nil, fmt.Errorf("%w: the query's address could be at several places", ErrTraceAmbiguousSource)
	}
	counts := map[string]any{"valid": 0, "dropped": 0, "blackhole": 0, "unreachable": 0, "loop": 0, "other": 0}
	for _, group := range facets.PathGroups {
		bucket, ok := flowBuckets[strings.ToUpper(group.TraceType)]
		if !ok {
			bucket = "other"
		}
		counts[bucket] = counts[bucket].(int) + len(group.Paths)
	}
	return counts, nil
}

// predict is the predicted-only section: the predicted snapshot's processing state and, when it ended badly, one failure row. Modeling
// diagnostics are not read in schema_version 1 (their routes are internal and gated), so modeling_problems is always empty.
func (r *changeReader) predict(_ context.Context, snapshot *Snapshot) (map[string]any, error) {
	state := strings.ToUpper(snapshot.State)
	failures := make([]any, 0)
	switch state {
	case "FAILED", "CANCELED", "TIMED_OUT", "RESTORE_FAILED":
		failures = append(failures, map[string]any{"device": "-", "message": "snapshot processing ended " + state})
	}
	return map[string]any{"processing_state": state, "failures": failures, "modeling_problems": []any{}}, nil
}

// securityRules reads each device of the change set's draft (or head commit) through the security-rules diff. A device that is not a
// firewall Forward's predict supports answers a 400 "Device is not supported for firewall predict" and contributes no rows.
func (r *changeReader) securityRules(ctx context.Context, changeSetID string) (map[string]any, error) {
	if changeSetID == "" {
		return nil, errors.New("change set not given")
	}
	predict := r.service.client.Predict
	var devices []string
	draft, _, err := predict.GetDraft(ctx, r.networkID, changeSetID)
	if err != nil {
		return nil, err
	}
	if draft.Draft != nil {
		for name := range draft.Draft.DeviceToChanges {
			devices = append(devices, name)
		}
	} else {
		head, _, err := predict.GetHeadCommit(ctx, r.networkID, changeSetID)
		if err != nil {
			return nil, err
		}
		for name := range head.DeviceToChanges {
			devices = append(devices, name)
		}
	}
	sort.Strings(devices)
	items := make([]any, 0)
	for _, device := range devices {
		diff, _, err := predict.SecurityRulesDiff(ctx, r.networkID, changeSetID, device)
		if err != nil {
			var apiErr *ErrorResponse
			if errors.As(err, &apiErr) && apiErr.Response != nil && apiErr.Response.StatusCode == http.StatusBadRequest && strings.Contains(apiErr.Message, "not supported for firewall predict") {
				continue
			}
			return nil, err
		}
		for _, entry := range diff.Entries {
			change := ""
			switch strings.ToUpper(entry.DiffType) {
			case "ADDED":
				change = "added"
			case "DELETED":
				change = "removed"
			case "MODIFIED":
				change = "modified"
			default:
				continue
			}
			rule := entry.B
			if rule == nil {
				rule = entry.A
			}
			if rule == nil {
				continue
			}
			items = append(items, map[string]any{"device": device, "rule": rule.ScopeID + "/" + rule.RulebaseID + "/" + rule.Definition.Name, "change": change})
		}
	}
	return map[string]any{"items": items}, nil
}
