package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A fake Forward for the diff routes the change report reads. The bodies are what Forward's controllers return (DiffController), written from
// the requirement in testdata/change_report/RULES.md ("Section contents from Forward's reads"), not from the reader's code.
func changeReportServer(t *testing.T, override func(path, query string) (int, string, bool)) *httptest.Server {
	t.Helper()
	if override == nil {
		return changeReportServerRequest(t, nil)
	}
	return changeReportServerRequest(t, func(r *http.Request, _ string) (int, string, bool) {
		return override(r.URL.Path+"?"+r.URL.RawQuery, "")
	})
}

func changeReportServerRequest(t *testing.T, override func(r *http.Request, body string) (int, string, bool)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query := r.URL.Path, r.URL.Query()
		if override != nil {
			raw, _ := io.ReadAll(r.Body)
			if status, body, ok := override(r, string(raw)); ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(body))
				return
			}
		}
		reply := func(body string) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}
		switch {
		case path == "/api/diffs/1/2/checks" && query.Get("type") == "Existential":
			reply(`[{"diffType":"MODIFIED","a":{"id":"C-1","name":"app reaches db","status":"PASS"},"b":{"id":"C-1","name":"app reaches db","status":"FAIL"}},
			        {"diffType":"UNCHANGED","a":{"id":"C-2","name":"mgmt up","status":"PASS"},"b":{"id":"C-2","name":"mgmt up","status":"PASS"}}]`)
		case path == "/api/diffs/1/2/checks" && query.Get("type") == "QueryStringBased":
			w.WriteHeader(http.StatusNotFound)
			reply(`{"message":"No endpoint GET /api/diffs/1/2/checks."}`)
		case path == "/api/diffs/1/2/checks":
			reply(`[]`)
		case path == "/api/diffs/1/2/subnet-connectivity" && query.Get("view") == "locationPairs":
			if query.Get("type") == "DELETED" {
				reply(`{"counts":[{"src":"site-b","dst":"site-a","count":3},{"src":"site-a","dst":"site-b","count":5}]}`)
				return
			}
			reply(`{"counts":[]}`)
		case path == "/api/diffs/1/2/subnet-connectivity":
			reply(`{"isPartialResult":false,"evaluatedSubnetPairs":40,"totalSubnetPairs":42}`)
		case path == "/api/diffs/1/2/l3":
			reply(`[{"deviceName":"edge-2","stats":{"DELETED":6,"UNCHANGED":9}},{"deviceName":"edge-1","stats":{"ADDED":1,"MODIFIED":2}},{"deviceName":"core","stats":{"UNCHANGED":4}}]`)
		case path == "/api/diffs/1/2/files":
			reply(`[{"name":"edge-2","files":[{"name":"edge-2,CONFIG.txt"}]}]`)
		case path == "/api/diffs/1/2/devices" && !query.Has("count"):
			reply(`[{"diffType":"MODIFIED","a":{"name":"edge-2"},"b":{"name":"edge-2"}},{"diffType":"ADDED","b":{"name":"edge-9"}},{"diffType":"DELETED","a":{"name":"edge-0"}},{"diffType":"UNCHANGED","a":{"name":"core"},"b":{"name":"core"}}]`)
		case path == "/api/diffs/1/2/vulnerabilities/counts":
			reply(`{"warnings":{"cveCount":2},"improvements":{"cveCount":1,"exposedVulnerableDevicesCount":0}}`)
		case path == "/api/diffs/1/2/routing-loop/count":
			reply(`{"count":0,"complete":true}`)
		case strings.HasPrefix(path, "/api/diffs/1/2/"):
			if path == "/api/diffs/1/2/cloud-objects" || path == "/api/diffs/1/2/cloud-acl" {
				w.WriteHeader(http.StatusNotFound)
				reply(`{"message":"No endpoint GET ` + path + `."}`)
				return
			}
			reply(`{"count":1,"complete":true}`)
		default:
			t.Errorf("unexpected request %s", r.URL.RequestURI())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func sectionOf(t *testing.T, report *ChangeReport, name string) map[string]any {
	t.Helper()
	return asMap(asMap(report.doc["sections"])[name])
}

// Compare reads the default sections of two collected snapshots and assembles them: a check that went PASS to FAIL is a regression, rows come
// from the routes RULES.md names, and what Forward does not serve is left out rather than failing the report.
func TestChangeReportsCompareCollected(t *testing.T) {
	server := changeReportServer(t, nil)
	defer server.Close()
	client := newTestClient(t, server.URL)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	report, err := client.ChangeReports.Compare(context.Background(), "n1", "1", "2", ChangeReportOptions{AfterKind: "collected", CreatedAt: created, SectionTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict() != ChangeReportRegression {
		t.Fatalf("verdict = %s, reasons = %+v", report.Verdict(), report.Reasons())
	}
	if reasons := report.Reasons(); len(reasons) != 1 || reasons[0].Code != "check_regressed" || reasons[0].Detail != "C-1" {
		t.Fatalf("reasons = %+v", reasons)
	}
	checks := sectionOf(t, report, "checks")
	if asInt(asMap(checks["counts"])["regressed"]) != 1 || asInt(asMap(checks["counts"])["unchanged"]) != 1 || asInt(checks["total"]) != 2 {
		t.Fatalf("checks = %+v", checks)
	}
	if conn := sectionOf(t, report, "connectivity"); conn["status"] != "ok" || conn["no_forwarding_change"] != false || asInt(conn["total"]) != 2 || asInt(conn["evaluated_subnet_pairs"]) != 40 || asInt(conn["total_subnet_pairs"]) != 42 {
		t.Fatalf("connectivity = %+v", conn)
	}
	routing := asList(sectionOf(t, report, "routing")["items"])
	if len(routing) != 2 || asString(asMap(routing[0])["device"]) != "edge-1" || asInt(asMap(routing[1])["removed"]) != 6 {
		t.Fatalf("routing (core has no change and must be absent): %+v", routing)
	}
	files := asList(asMap(asList(sectionOf(t, report, "config")["items"])[0])["files_changed"])
	if len(files) != 1 || asString(files[0]) != "CONFIG.txt" {
		t.Fatalf("config files must use the short name without the device prefix: %+v", files)
	}
	devices := sectionOf(t, report, "devices")
	if len(asList(devices["added"])) != 1 || len(asList(devices["modified"])) != 1 || len(asList(devices["removed"])) != 1 || asString(asList(devices["removed"])[0]) != "edge-0" {
		t.Fatalf("devices = %+v", devices)
	}
	families := asMap(sectionOf(t, report, "diff_counts")["families"])
	if _, present := families["cloud_objects"]; present || families["routing_loop"] == nil || families["devices"] == nil {
		t.Fatalf("a family Forward does not serve is omitted, the rest kept: %+v", families)
	}
	vulnerabilities := sectionOf(t, report, "vulnerabilities")
	if asInt(vulnerabilities["introduced"]) != 2 || asInt(vulnerabilities["resolved"]) != 1 || vulnerabilities["exposed_devices"] != nil {
		t.Fatalf("vulnerabilities = %+v", vulnerabilities)
	}
	if sectionOf(t, report, "predict")["status"] != "not_applicable" || sectionOf(t, report, "flows")["status"] != "not_requested" {
		t.Fatal("predict is not applicable to a collected after, flows are opt-in")
	}
	if !strings.Contains(report.Markdown(), "**Verdict: regression**") {
		t.Fatal("the Markdown must carry the verdict")
	}
}

// A refused read never reads as "no change": a non-driving section becomes unavailable with a typed reason and leaves the verdict alone; the
// checks section, which drives the verdict, makes it incomplete.
func TestChangeReportsCompareUnavailableSections(t *testing.T) {
	server := changeReportServer(t, func(path, _ string) (int, string, bool) {
		switch {
		case strings.HasPrefix(path, "/api/diffs/1/2/subnet-connectivity"):
			return http.StatusForbidden, `{"message":"LOCATION_CONNECTIVITY_DIFFS is off for your organization"}`, true
		case strings.HasPrefix(path, "/api/diffs/1/2/vulnerabilities"):
			return http.StatusForbidden, `{"message":"Missing permission: NetworkOperation.VIEW_CVE_DIFFS"}`, true
		case strings.HasPrefix(path, "/api/diffs/1/2/l3"):
			return http.StatusNotFound, `{"message":"No endpoint GET /api/diffs/1/2/l3."}`, true
		}
		return 0, "", false
	})
	defer server.Close()
	client := newTestClient(t, server.URL)
	report, err := client.ChangeReports.Compare(context.Background(), "n1", "1", "2", ChangeReportOptions{AfterKind: "collected", SectionTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{"connectivity": "feature_gated", "vulnerabilities": "forbidden", "routing": "not_served"}
	for name, kind := range kinds {
		section := sectionOf(t, report, name)
		if section["status"] != "unavailable" || asString(asMap(section["reason"])["kind"]) != kind {
			t.Errorf("%s = %+v, want unavailable(%s)", name, section, kind)
		}
	}
	if report.Verdict() != ChangeReportRegression {
		t.Fatalf("non-driving unavailable sections must not change the verdict: %s %+v", report.Verdict(), report.Reasons())
	}

	// The checks section drives the verdict: refused → incomplete, never no_regression.
	denied := changeReportServer(t, func(path, _ string) (int, string, bool) {
		if strings.HasPrefix(path, "/api/diffs/1/2/checks") {
			return http.StatusForbidden, `{"message":"Missing permission: NetworkOperation.VIEW_CHECK_DIFFS"}`, true
		}
		return 0, "", false
	})
	defer denied.Close()
	incomplete, err := newTestClient(t, denied.URL).ChangeReports.Compare(context.Background(), "n1", "1", "2", ChangeReportOptions{AfterKind: "collected", SectionTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if incomplete.Verdict() != ChangeReportIncomplete {
		t.Fatalf("verdict = %s, want incomplete", incomplete.Verdict())
	}
}

// Compare fails only when it cannot build a report at all.
func TestChangeReportsCompareRefusesWhatCannotBeBuilt(t *testing.T) {
	server := changeReportServer(t, nil)
	defer server.Close()
	client := newTestClient(t, server.URL)
	if _, err := client.ChangeReports.Compare(context.Background(), "n1", "", "2", ChangeReportOptions{}); err == nil {
		t.Fatal("a missing base snapshot must be refused")
	}
	if _, err := client.ChangeReports.Compare(context.Background(), "n1", "1", "2", ChangeReportOptions{AfterKind: "future"}); err == nil {
		t.Fatal("an unknown after kind must be refused")
	}
}

func facetsBody(groups string, extra string) string {
	return `{"facets":{"pathGroups":` + groups + `},"totalResults":{"value":1,"type":"EXACT"}` + extra + `}`
}

// Flows (opt-in) compare a named path query on both snapshots: a bucket's count is the number of distinct device paths in that FlowType group,
// a lost VALID path regresses a "valid" flow, and a search that timed out or only counted a lower bound makes that flow unavailable, never "none".
func TestChangeReportsCompareFlows(t *testing.T) {
	flowsBySnapshot := map[string]string{}
	server := changeReportServer(t, func(path, _ string) (int, string, bool) {
		if path == "/api/networks/n1/trace-facets?snapshotId=1" || path == "/api/networks/n1/trace-facets?snapshotId=2" {
			return http.StatusOK, flowsBySnapshot[path[len(path)-1:]], true
		}
		return 0, "", false
	})
	defer server.Close()
	client := newTestClient(t, server.URL)
	query := TracePathQuery{From: &TraceEndpoint{Location: TraceDevice("a")}, To: &TraceEndpoint{Location: TraceDevice("b")}}
	flows := []ChangeReportFlow{{Name: "a to b", Expectation: "valid", Query: query}}
	compare := func() *ChangeReport {
		report, err := client.ChangeReports.Compare(context.Background(), "n1", "1", "2", ChangeReportOptions{AfterKind: "collected", Flows: flows, SectionTimeout: 10 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		return report
	}

	flowsBySnapshot["1"] = facetsBody(`[{"traceType":"VALID","paths":[{"path":["a","m","b"]},{"path":["a","n","b"]}]},{"traceType":"INADMISSIBLE","paths":[{"path":["a"]}]}]`, "")
	flowsBySnapshot["2"] = facetsBody(`[{"traceType":"DROPPED","paths":[{"path":["a","m"]}]}]`, "")
	report := compare()
	item := asMap(asList(sectionOf(t, report, "flows")["items"])[0])
	base, after := asMap(item["base"]), asMap(item["after"])
	if asInt(base["valid"]) != 2 || asInt(base["other"]) != 1 || asInt(after["valid"]) != 0 || asInt(after["dropped"]) != 1 || asString(item["change"]) != "lost_valid" {
		t.Fatalf("flow = %+v (paths per group, INADMISSIBLE counted as other)", item)
	}
	lostReason := false
	for _, r := range report.Reasons() {
		lostReason = lostReason || (r.Code == "flow_lost_valid_path" && r.Detail == "a to b")
	}
	if report.Verdict() != ChangeReportRegression || !lostReason {
		t.Fatalf("verdict %s reasons %+v", report.Verdict(), report.Reasons())
	}

	flowsBySnapshot["2"] = facetsBody(`[]`, `,"timedOut":true`)
	timedOut := asMap(asList(sectionOf(t, compare(), "flows")["items"])[0])
	if asString(timedOut["change"]) != "unavailable" || asString(asMap(timedOut["unavailable"])["kind"]) != "timeout" || compare().Verdict() != ChangeReportIncomplete {
		t.Fatalf("a timed-out search must be an unavailable flow and an incomplete report: %+v", timedOut)
	}
	flowsBySnapshot["2"] = `{"facets":{"pathGroups":[]},"totalResults":{"value":5000,"type":"LOWER_BOUND"}}`
	lower := asMap(asList(sectionOf(t, compare(), "flows")["items"])[0])
	if asString(lower["change"]) != "unavailable" {
		t.Fatalf("a lower-bound total is not a count: %+v", lower)
	}
}

// Predicted pair: predict reads the snapshot state and security_rules the change set's devices; a device Forward's firewall predict does not
// support (400) contributes no rows, a missing change set id makes the section unavailable.
func TestChangeReportsComparePredicted(t *testing.T) {
	state := "PROCESSED"
	server := changeReportServer(t, func(path, _ string) (int, string, bool) {
		switch {
		case strings.HasPrefix(path, "/api/networks/n1/snapshots"):
			return http.StatusOK, `{"snapshots":[{"id":"2","state":"` + state + `","parentSnapshotId":"1"}]}`, true
		case strings.HasPrefix(path, "/api/networks/n1/change-sets/cs1/draft") && !strings.Contains(path, "/devices/"):
			return http.StatusOK, `{"draft":{"id":"cs1","deviceToChanges":{"fw-1":{},"sw-1":{}}}}`, true
		case strings.HasPrefix(path, "/api/networks/n1/change-sets/cs1/devices/sw-1/security-rules-diff"):
			return http.StatusBadRequest, `{"message":"Device is not supported for firewall predict"}`, true
		case strings.HasPrefix(path, "/api/networks/n1/change-sets/cs1/devices/fw-1/security-rules-diff"):
			return http.StatusOK, `{"rulebases":[],"entries":[
			  {"diffType":"ADDED","b":{"scopeId":"LOCAL","rulebaseId":"PRIMARY","definition":{"name":"allow-app"}}},
			  {"diffType":"DELETED","a":{"scopeId":"LOCAL","rulebaseId":"PRIMARY","definition":{"name":"old"}}},
			  {"diffType":"UNCHANGED","a":{"scopeId":"LOCAL","rulebaseId":"PRIMARY","definition":{"name":"keep"}},"b":{"scopeId":"LOCAL","rulebaseId":"PRIMARY","definition":{"name":"keep"}}}]}`, true
		}
		return 0, "", false
	})
	defer server.Close()
	client := newTestClient(t, server.URL)
	report, err := client.ChangeReports.Compare(context.Background(), "n1", "1", "2", ChangeReportOptions{ChangeSetID: "cs1", SectionTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	meta := asMap(report.doc["meta"])
	if asString(meta["after_kind"]) != "predicted" || asString(meta["change_set_id"]) != "cs1" {
		t.Fatalf("meta = %+v", meta)
	}
	rules := asList(sectionOf(t, report, "security_rules")["items"])
	if len(rules) != 2 || asString(asMap(rules[0])["rule"]) != "LOCAL/PRIMARY/allow-app" || asString(asMap(rules[0])["change"]) != "added" || asString(asMap(rules[1])["change"]) != "removed" {
		t.Fatalf("security rules (UNCHANGED dropped, the unsupported device skipped): %+v", rules)
	}
	predict := sectionOf(t, report, "predict")
	if asString(predict["processing_state"]) != "PROCESSED" || len(asList(predict["failures"])) != 0 || len(asList(predict["modeling_problems"])) != 0 {
		t.Fatalf("predict = %+v", predict)
	}

	state = "FAILED"
	failed, err := client.ChangeReports.Compare(context.Background(), "n1", "1", "2", ChangeReportOptions{SectionTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	failures := asList(sectionOf(t, failed, "predict")["failures"])
	if len(failures) != 1 || asString(asMap(failures[0])["message"]) != "snapshot processing ended FAILED" {
		t.Fatalf("a failed predicted snapshot must say so: %+v", failures)
	}
	if section := sectionOf(t, failed, "security_rules"); section["status"] != "unavailable" || asString(asMap(section["reason"])["message"]) != "change set not given" {
		t.Fatalf("no change set id: %+v", section)
	}
}

// State (opt-in): the four NQE families are read on BOTH snapshots and keyed as RULES.md says; a session that was ESTABLISHED and went IDLE, an
// admin-down interface and a lost LLDP adjacency are lost (regression), an ACL entry-count change is only reported, and an expected removal
// does not regress.
func TestChangeReportsCompareState(t *testing.T) {
	rows := map[string]map[string]string{
		"1": {
			"bgp":  `[{"Device":"core-1","VRF":"default","NeighborIP":"192.0.2.1","RemoteAS":65001,"SessionState":"ESTABLISHED"},{"Device":"core-1","VRF":"default","NeighborIP":"192.0.2.9","RemoteAS":65002,"SessionState":"ESTABLISHED"}]`,
			"lldp": `[{"Device":"core-1","LocalIntf":"et1","RemoteSys":"edge-1","RemotePort":"et49"}]`,
			"if":   `[{"Device":"edge-1","Interface":"et3","AdminStatus":"AdminStatus.UP"}]`,
			"acl":  `[{"Device":"edge-1","AclName":"in","Action":"AclAction.PERMIT"},{"Device":"edge-1","AclName":"in","Action":"AclAction.DENY"}]`,
		},
		"2": {
			"bgp":  `[{"Device":"core-1","VRF":"default","NeighborIP":"192.0.2.1","RemoteAS":65001,"SessionState":"IDLE"}]`,
			"lldp": `[]`,
			"if":   `[{"Device":"edge-1","Interface":"et3","AdminStatus":"AdminStatus.DOWN"}]`,
			"acl":  `[{"Device":"edge-1","AclName":"in","Action":"AclAction.PERMIT"}]`,
		},
	}
	server := changeReportServerRequest(t, func(r *http.Request, body string) (int, string, bool) {
		if r.URL.Path != "/api/nqe" {
			return 0, "", false
		}
		family := "acl"
		switch {
		case strings.Contains(body, "protocol.bgp"):
			family = "bgp"
		case strings.Contains(body, "lldp.neighbors"):
			family = "lldp"
		case strings.Contains(body, "adminStatus"):
			family = "if"
		}
		list := rows[r.URL.Query().Get("snapshotId")][family]
		total := strings.Count(list, `{"Device"`)
		return http.StatusOK, `{"snapshotId":"` + r.URL.Query().Get("snapshotId") + `","totalNumItems":` + itoa(total) + `,"items":` + list + `}`, true
	})
	defer server.Close()
	client := newTestClient(t, server.URL)
	compare := func(expected ...string) *ChangeReport {
		report, err := client.ChangeReports.Compare(context.Background(), "n1", "1", "2", ChangeReportOptions{
			AfterKind: "collected", SectionTimeout: 10 * time.Second, State: &ChangeReportState{ExpectedRemoved: expected},
		})
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	report := compare()
	items := asList(sectionOf(t, report, "state")["items"])
	got := map[string]string{}
	for _, raw := range items {
		m := asMap(raw)
		got[asString(m["kind"])+":"+strings.Join(identityKey(m["identity"]), "|")] = asString(m["change"])
	}
	want := map[string]string{
		"bgp_session:core-1|default|192.0.2.1": "lost", "bgp_session:core-1|default|192.0.2.9": "lost",
		"lldp_adjacency:core-1|et1|edge-1|et49": "lost", "interface_admin:edge-1|et3": "lost", "acl_entries:edge-1|in": "changed",
	}
	for _, raw := range items {
		if m := asMap(raw); asString(m["kind"]) == "lldp_adjacency" && asString(m["base"]) != "present" {
			t.Errorf("an LLDP adjacency's value is the word present: %+v", m)
		}
	}
	for key, change := range want {
		if got[key] != change {
			t.Errorf("%s = %q, want %q (all: %v)", key, got[key], change, got)
		}
	}
	stateLost := func(r *ChangeReport) int {
		n := 0
		for _, reason := range r.Reasons() {
			if reason.Code == "state_lost" {
				n++
			}
		}
		return n
	}
	if report.Verdict() != ChangeReportRegression || stateLost(report) != 4 {
		t.Fatalf("verdict %s reasons %+v", report.Verdict(), report.Reasons())
	}
	expected := compare("bgp_session:core-1|default|192.0.2.9", "bgp_session:core-1|default|192.0.2.1", "lldp_adjacency:core-1|et1|edge-1|et49", "interface_admin:edge-1|et3")
	if stateLost(expected) != 0 {
		t.Fatalf("expected removals must not be reasons: %+v", expected.Reasons())
	}
	for _, raw := range asList(sectionOf(t, expected, "state")["items"]) {
		if m := asMap(raw); asString(m["change"]) == "lost" && m["expected"] != true {
			t.Fatalf("a lost item that was expected must be flagged: %+v", m)
		}
	}
}

// Only PROCESSING is still computing: a diff that keeps answering it past the section budget makes the checks section unavailable (timeout).
// NONE and REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING do not clear by waiting (the second needs advanced reachability that may never run), so they
// are judged per check: the row is `other`, counted as not evaluated, a PASS base gives the reason check_not_evaluated and the verdict is
// incomplete, while the section and every other check stay readable.
func TestChangeReportsCompareNotFinishedChecks(t *testing.T) {
	status := "NONE"
	server := changeReportServer(t, func(path, _ string) (int, string, bool) {
		if strings.HasPrefix(path, "/api/diffs/1/2/checks?type=Existential") {
			if status == "PROCESSING" {
				return http.StatusOK, `[{"diffType":"PROCESSING","a":{"id":"C-1","name":"n","status":"PROCESSING"}}]`, true
			}
			return http.StatusOK, `[{"diffType":"MODIFIED","a":{"id":"C-1","name":"n","status":"PASS"},"b":{"id":"C-1","name":"n","status":"` + status + `"}},
			  {"diffType":"MODIFIED","a":{"id":"C-2","name":"m","status":"PASS"},"b":{"id":"C-2","name":"m","status":"FAIL"}}]`, true
		}
		return 0, "", false
	})
	defer server.Close()
	client := newTestClient(t, server.URL)
	for _, st := range []string{"NONE", "REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING"} {
		status = st
		report, err := client.ChangeReports.Compare(context.Background(), "n1", "1", "2", ChangeReportOptions{AfterKind: "collected", SectionTimeout: 300 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		section := sectionOf(t, report, "checks")
		counts := asMap(section["counts"])
		if section["status"] != "ok" || asInt(counts["not_evaluated"]) != 1 || asInt(counts["other"]) != 1 || asInt(counts["regressed"]) != 1 || report.Verdict() != ChangeReportIncomplete {
			t.Errorf("%s: checks = %+v, verdict %s", st, section, report.Verdict())
		}
		codes := map[string]string{}
		for _, r := range report.Reasons() {
			codes[r.Code] = r.Detail
		}
		if codes["check_not_evaluated"] != "C-1" || codes["check_regressed"] != "C-2" {
			t.Errorf("%s: the unevaluable check and the regression must both be listed: %+v", st, report.Reasons())
		}
	}
	status = "PROCESSING"
	report, err := client.ChangeReports.Compare(context.Background(), "n1", "1", "2", ChangeReportOptions{AfterKind: "collected", SectionTimeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if section := sectionOf(t, report, "checks"); section["status"] != "unavailable" || asString(asMap(section["reason"])["kind"]) != "timeout" || report.Verdict() != ChangeReportIncomplete {
		t.Errorf("PROCESSING past the budget: checks = %+v, verdict %s", section, report.Verdict())
	}
}
