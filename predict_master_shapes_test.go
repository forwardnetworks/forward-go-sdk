package forward

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Forward requires a note on a commit (a missing one is a 400 on every build), so a blank one is sent as the default; the analysis mode is
// sent only when asked for; the answer is the commit's metadata.
func TestCommitSendsANoteAndTheMode(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	predict := newTestClient(t, predictRecorder(t, 200, `{"commitId":"c1","note":"n","committedById":"u","committedBy":"alice","committedAt":"2026-11-02T00:00:00Z"}`, &seen).URL).Predict
	ctx := context.Background()
	if _, err := predict.Commit(ctx, "7", "3", "  "); err != nil {
		t.Fatal(err)
	}
	meta, _, err := predict.CommitWithOptions(ctx, "7", "3", CommitOptions{Note: "go live", AnalysisMode: AnalysisModeConfigOnly})
	if err != nil || meta.CommitID != "c1" || meta.CommittedBy != "alice" {
		t.Fatalf("meta = %+v, %v", meta, err)
	}
	if _, _, err := predict.CommitWithOptions(ctx, "7", "3", CommitOptions{AnalysisMode: "FAST"}); err == nil || len(seen) != 2 {
		t.Fatal("an unknown analysis mode was sent")
	}
	if seen[0].method != "POST" || seen[0].uri != "/api/networks/7/change-sets/3/commits?note=Committed+by+forward-go-sdk" {
		t.Errorf("blank note request = %+v", seen[0])
	}
	if seen[1].uri != "/api/networks/7/change-sets/3/commits?analysisMode=CONFIG_ONLY&note=go+live" {
		t.Errorf("mode request = %+v", seen[1])
	}
}

// Master's shapes decode: the change set's mode and attribution, the list row's modified-device count and predicted snapshots, a
// predicted snapshot's predictInfo, a command error's applied flag, and a rule's lifecycle data.
func TestMasterShapesDecode(t *testing.T) {
	t.Parallel()
	var cs ChangeSet
	in := `{"id":"3","name":"n","networkId":"7","snapshotId":"s1","analysisMode":"CONFIG_ONLY","createdById":"u","createdBy":"alice","createdAt":"2026-11-01T00:00:00Z","updatedBy":"bob","modifiedDeviceCount":2,"predictedSnapshots":[{"id":"p2","creationInstant":"2026-11-02T00:00:00Z","networkId":"7"},{"id":"p1"}]}`
	if err := json.Unmarshal([]byte(in), &cs); err != nil || cs.AnalysisMode != AnalysisModeConfigOnly || cs.CreatedBy != "alice" || cs.UpdatedBy != "bob" ||
		cs.ModifiedDeviceCount == nil || *cs.ModifiedDeviceCount != 2 || len(cs.PredictedSnapshots) != 2 || cs.PredictedSnapshots[0].ID != "p2" {
		t.Fatalf("change set = %+v, %v", cs, err)
	}
	var snap Snapshot
	if err := json.Unmarshal([]byte(`{"id":"p2","state":"PROCESSED","processingTrigger":"PREDICT","predictInfo":{"baseSnapshot":{"id":"s1","creationInstant":"2026-11-01T00:00:00Z","networkId":"7"},"changeSetId":"3","commitId":"c1","name":"n","modifiedDeviceCount":2,"committedAt":"2026-11-02T00:00:00Z","analysisMode":"BEHAVIORAL"}}`), &snap); err != nil ||
		snap.PredictInfo == nil || snap.PredictInfo.BaseSnapshot == nil || snap.PredictInfo.BaseSnapshot.ID != "s1" || snap.PredictInfo.CommitID != "c1" || snap.PredictInfo.AnalysisMode != AnalysisModeBehavioral || snap.PredictInfo.ModifiedDeviceCount != 2 {
		t.Fatalf("snapshot = %+v, %v", snap, err)
	}
	var ce CommandError
	if err := json.Unmarshal([]byte(`{"lineNumber":2,"errorType":"UNSUPPORTED","errorMsg":"x","applied":true}`), &ce); err != nil || ce.Applied == nil || !*ce.Applied {
		t.Fatalf("command error = %+v, %v", ce, err)
	}
	var rule ScopedSecurityRule
	if err := json.Unmarshal([]byte(`{"scopeId":"LOCAL","rulebaseId":"PRIMARY","rank":1,"definition":{"name":"r"},"createdAt":"2026-01-01T00:00:00Z","lastUsed":"2026-10-01T00:00:00Z","hitCount":5}`), &rule); err != nil ||
		rule.CreatedAt == "" || rule.LastUsed == "" || rule.HitCount == nil || *rule.HitCount != 5 {
		t.Fatalf("rule = %+v, %v", rule, err)
	}
}

// Clearing one device's changes is a different route from discarding the caller's draft edits, and trigger and retrigger are the two
// actions of protocol modeling.
func TestDiscardAllAndProtocolModelingActions(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	predict := newTestClient(t, predictRecorder(t, 204, ``, &seen).URL).Predict
	ctx := context.Background()
	if _, err := predict.DiscardAllDeviceChanges(ctx, "7", "3", "edge 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := predict.TriggerProtocolModeling(ctx, "s9"); err != nil {
		t.Fatal(err)
	}
	if _, err := predict.RetriggerProtocolModeling(ctx, "s9"); err != nil {
		t.Fatal(err)
	}
	if _, err := predict.DiscardAllDeviceChanges(ctx, "7", "3", " "); err == nil {
		t.Fatal("a blank device was sent")
	}
	got := []string{}
	for _, c := range seen {
		got = append(got, c.method+" "+c.uri)
	}
	want := "POST /api/networks/7/change-sets/3/devices/edge%201?action=discardChanges|POST /api/internal/snapshots/s9/protocol-modeling?action=trigger|POST /api/internal/snapshots/s9/protocol-modeling?action=retrigger"
	if strings.Join(got, "|") != want {
		t.Fatalf("calls = %v", got)
	}
}

// The rp-prefix-* methods send master's bodies: a protocol on the router and origin, a node as key plus route set id; a hop from a trace
// becomes a node ref only when it carries a key.
func TestRPPrefixShapes(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	trace := `[[{"device":"r1","vrf":"default","routeSetId":4,"protocol":"OMP","nodeKey":{"protocol":"OMP","device":"r1","vrf":"default"}}]]`
	routing := newTestClient(t, predictRecorder(t, 200, trace, &seen).URL).Routing
	ctx := context.Background()

	paths, _, err := routing.RPPrefixTrace(ctx, "7", "s1", RPPrefixTrace{Prefix: "10.0.0.0/8", Router: DeviceVRFProtocol{Device: "r1", Protocol: "OMP"}, OriginatedFrom: DeviceVRFProtocol{Device: "r2", Protocol: "OMP"}, Outcome: RouteOutcomeInstalled})
	if err != nil || len(paths) != 1 {
		t.Fatalf("paths = %+v, %v", paths, err)
	}
	ref, err := paths[0][0].Ref()
	if err != nil || ref.RouteSetID != 4 || !strings.Contains(string(ref.NodeKey), `"OMP"`) {
		t.Fatalf("ref = %+v, %v", ref, err)
	}
	if _, err := (RoutingNode{Device: "r1"}).Ref(); err == nil {
		t.Error("a node without a key made a ref")
	}
	if _, _, err := routing.RPPrefixSearch(ctx, "7", "s1", RPPrefixSearch{Prefix: "10.0.0.0/8", Router: &DeviceVRFProtocol{Device: "r1"}}); err == nil {
		t.Error("a router without a protocol was sent")
	}
	if _, _, err := routing.RPPrefixDetails(ctx, "7", "s1", RPPrefixDetailsRequest{Prefix: "10.0.0.0/8", Node: RPNodeRef{RouteSetID: 1}}); err == nil {
		t.Error("a node without a key was sent")
	}
	searcher := newTestClient(t, predictRecorder(t, 200, `{"origin":[],"devicesByOutcome":{}}`, &seen).URL).Routing
	if _, _, err := searcher.RPPrefixSearch(ctx, "7", "s1", RPPrefixSearch{Prefix: "10.0.0.0/8", Router: &DeviceVRFProtocol{Device: "r1", Protocol: "IS_IS"}, Outcome: RouteOutcomeInstalled}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("seen %+v", seen)
	}
	if seen[0].uri != "/api/networks/7/rp-prefix-trace?snapshotId=s1" || seen[0].body != `{"prefix":"10.0.0.0/8","router":{"device":"r1","protocol":"OMP"},"originatedFrom":{"device":"r2","protocol":"OMP"},"outcome":"INSTALLED"}` {
		t.Errorf("trace request = %+v", seen[0])
	}
	if seen[1].body != `{"prefix":"10.0.0.0/8","router":{"device":"r1","protocol":"IS_IS"},"outcome":"INSTALLED"}` {
		t.Errorf("search request = %+v", seen[1])
	}

	detail := newTestClient(t, predictRecorder(t, 200, `{"prefix":"10.0.0.0/8","nodeKey":{"protocol":"OMP","device":"r1"},"attributes":{"protocol":"OMP"},"policies":{"IMPORT":[]}}`, &seen).URL).Routing
	d, _, err := detail.RPPrefixDetails(ctx, "7", "s1", RPPrefixDetailsRequest{Prefix: "10.0.0.0/8", Node: ref})
	if err != nil || !strings.Contains(string(d.NodeKey), "r1") || d.Policies["IMPORT"] == nil {
		t.Fatalf("details = %+v, %v", d, err)
	}
	if last := seen[len(seen)-1]; last.uri != "/api/networks/7/rp-prefix-details?snapshotId=s1" || !strings.Contains(last.body, `"node":{"nodeKey":{"protocol":"OMP","device":"r1","vrf":"default"},"routeSetId":4}`) {
		t.Errorf("details request = %+v", last)
	}

	info := newTestClient(t, predictRecorder(t, 200, `{"device":"r1","vendor":"cisco","model":"x","vrfs":[{"vrf":"default","recvAdvertisements":[{"nexthop":"10.0.0.1/32","protocol":"IS_IS","outcome":"INSTALLED","originatedFrom":{"device":"r2","vrf":"default","protocol":"IS_IS"}}],"moreSpecificInstalledRoutes":[]}]}`, &seen).URL).Routing
	pi, _, err := info.RPDevicePrefixInfo(ctx, "7", "s1", "r1", "10.0.0.0/8")
	if err != nil || pi.VRFs[0].RecvAdvertisements[0].Protocol != "IS_IS" || pi.VRFs[0].RecvAdvertisements[0].OriginatedFrom.Protocol != "IS_IS" {
		t.Fatalf("info = %+v, %v", pi, err)
	}
	if last := seen[len(seen)-1]; last.uri != "/api/networks/7/devices/r1?prefix=10.0.0.0%2F8&snapshotId=s1&view=rp-prefix-info" {
		t.Errorf("info request = %+v", last)
	}
}

// A build that lacks view=rp-prefix-info answers the plain device with 200: that must not read as "no routes". A bare-string base snapshot
// (an older shape) decodes next to the object Forward sends today.
func TestRPDevicePrefixInfoDetectsAnUnservedView(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	routing := newTestClient(t, predictRecorder(t, 200, `{"name":"r1","vendor":"cisco_ios","interfaces":[]}`, &seen).URL).Routing
	if _, _, err := routing.RPDevicePrefixInfo(context.Background(), "7", "s1", "r1", "10.0.0.0/8"); !errors.Is(err, ErrEndpointNotServed) {
		t.Fatalf("err = %v, want ErrEndpointNotServed", err)
	}
	var info SnapshotPredictInfo
	if err := json.Unmarshal([]byte(`{"baseSnapshot":"s1","changeSetId":"3"}`), &info); err != nil || info.BaseSnapshot == nil || info.BaseSnapshot.ID != "s1" {
		t.Fatalf("info = %+v, %v", info, err)
	}
}
