package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The ServiceNow change request calls: the input takes Forward's enum NAMES while the answer carries ServiceNow's display strings; the
// attachment goes up as the multipart part "file" with its name; refusals never reach the wire.
func TestServiceNowChangeRequests(t *testing.T) {
	t.Parallel()
	var uploaded struct {
		part, name, content string
	}
	var seen []predictSeen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, predictSeen{r.Method, r.URL.RequestURI(), string(b), r.Header.Get("Content-Type")})
		switch {
		case strings.HasSuffix(r.URL.Path, "/attachments") && r.Method == "POST":
			_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			part, err := multipart.NewReader(bytes.NewReader(b), params["boundary"]).NextPart()
			if err == nil {
				data, _ := io.ReadAll(part)
				uploaded.part, uploaded.name, uploaded.content = part.FormName(), part.FileName(), string(data)
			}
			w.WriteHeader(204)
		case strings.Contains(r.URL.Path, "/attachments/"):
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "FILEBYTES")
		default:
			_, _ = io.WriteString(w, `{"number":"CHG001","sysId":"abc","summary":"s","impact":"2 - Medium","priority":"3 - Moderate","state":"Assess","url":"https://x.service-now.com/nav_to.do?uri=1","attachments":[{"fileName":"plan.pdf","fileSysId":"f1"}]}`)
		}
	}))
	defer server.Close()
	in := newTestClient(t, server.URL).Integrations
	ctx := context.Background()

	cr, _, err := in.CreateChangeSetChangeRequest(ctx, "7", "3", ServiceNowChangeRequestInput{Summary: "s", Impact: ServiceNowImpactMedium, Priority: ServiceNowPriorityModerate, WorkNotes: "n"})
	if err != nil || cr.Number != "CHG001" || cr.SysID != "abc" || cr.Impact != "2 - Medium" || cr.State != "Assess" || cr.Attachments[0].FileSysID != "f1" {
		t.Fatalf("created = %+v, %v", cr, err)
	}
	if _, _, err := in.GetChangeSetChangeRequest(ctx, "7", "3", "abc"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.AttachToChangeSetChangeRequest(ctx, "7", "3", "abc", "plan.pdf", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := in.DownloadChangeSetChangeRequestAttachment(ctx, "7", "3", "abc", "f 1", &out); err != nil || out.String() != "FILEBYTES" {
		t.Fatalf("download = %q, %v", out.String(), err)
	}
	if uploaded.part != "file" || uploaded.name != "plan.pdf" || uploaded.content != "hello" {
		t.Errorf("upload = %+v", uploaded)
	}
	want := []string{
		"POST /api/networks/7/change-sets/3/servicenow/change-requests",
		"GET /api/networks/7/change-sets/3/servicenow/change-requests/abc",
		"POST /api/networks/7/change-sets/3/servicenow/change-requests/abc/attachments",
		"GET /api/networks/7/change-sets/3/servicenow/change-requests/abc/attachments/f%201",
	}
	for i, w := range want {
		if seen[i].method+" "+seen[i].uri != w {
			t.Errorf("request %d = %s %s, want %s", i, seen[i].method, seen[i].uri, w)
		}
	}
	if seen[0].body != `{"summary":"s","impact":"MEDIUM","priority":"MODERATE","workNotes":"n"}` {
		t.Errorf("create body = %s", seen[0].body)
	}
	before := len(seen)
	for name, err := range map[string]error{
		"no summary": func() error {
			_, _, err := in.CreateChangeSetChangeRequest(ctx, "7", "3", ServiceNowChangeRequestInput{Impact: ServiceNowImpactLow})
			return err
		}(),
		"bad impact": func() error {
			_, _, err := in.CreateChangeSetChangeRequest(ctx, "7", "3", ServiceNowChangeRequestInput{Summary: "s", Impact: "high"})
			return err
		}(),
		"bad priority": func() error {
			_, _, err := in.CreateChangeSetChangeRequest(ctx, "7", "3", ServiceNowChangeRequestInput{Summary: "s", Impact: "LOW", Priority: "URGENT"})
			return err
		}(),
		"no sys id": func() error { _, _, err := in.GetChangeSetChangeRequest(ctx, "7", "3", " "); return err }(),
		"empty file": func() error {
			_, err := in.AttachToChangeSetChangeRequest(ctx, "7", "3", "abc", "a.txt", nil)
			return err
		}(),
		"too big": func() error {
			_, err := in.AttachToChangeSetChangeRequest(ctx, "7", "3", "abc", "a", make([]byte, ServiceNowMaxAttachmentBytes+1))
			return err
		}(),
		"no writer": func() error {
			_, err := in.DownloadChangeSetChangeRequestAttachment(ctx, "7", "3", "abc", "f", nil)
			return err
		}(),
		"no file sysid": func() error {
			_, err := in.DownloadChangeSetChangeRequestAttachment(ctx, "7", "3", "abc", "", &out)
			return err
		}(),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if len(seen) != before {
		t.Fatalf("a refused call reached the wire: %+v", seen[before:])
	}
}

// Unit packet tests and the model verification take the snapshot in the path; the protocol-modeling debug reads need a prefix and refuse a
// VRF without a device.
func TestIEntryChecksAndProtocolModelingReads(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	predict := newTestClient(t, predictRecorder(t, 200, `[{"name":"c1","deviceName":"r1","entry":{"x":1},"entryType":"IPv4","checkType":"exists","status":"FAIL","description":"missing"}]`, &seen).URL).Predict
	ctx := context.Background()
	results, _, err := predict.RunIEntryChecks(ctx, "s 1", []IEntryCheck{{Name: "c1", DeviceName: "r1", Entry: []byte(`{"x":1}`), EntryType: IEntryTypeIPv4, CheckType: IEntryCheckExists}})
	if err != nil || len(results) != 1 || results[0].Status != "FAIL" || results[0].Name != "c1" || results[0].Description != "missing" {
		t.Fatalf("results = %+v, %v", results, err)
	}
	if _, _, err := predict.RunIEntryChecks(ctx, "s1", nil); err == nil {
		t.Error("no checks was sent")
	}
	// Forward marks any check type but "exists" and "notexists" as PASS, so the SDK must not send one.
	for _, bad := range []IEntryCheck{
		{Name: "up", DeviceName: "r1", Entry: []byte(`{}`), EntryType: IEntryTypeIPv4, CheckType: "EXISTS"},
		{Name: "tbl", DeviceName: "r1", Entry: []byte(`{}`), EntryType: "MPLS", CheckType: IEntryCheckExists},
		{Name: "dev", Entry: []byte(`{}`), EntryType: IEntryTypeIPv4, CheckType: IEntryCheckExists},
	} {
		if _, _, err := predict.RunIEntryChecks(ctx, "s1", []IEntryCheck{bad}); err == nil {
			t.Errorf("check %q was sent", bad.Name)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("a refused check reached the wire: %+v", seen)
	}
	if seen[0].uri != "/api/snapshots/s%201/ientrychecks" || seen[0].body != `[{"name":"c1","deviceName":"r1","entry":{"x":1},"entryType":"IPv4","checkType":"exists"}]` {
		t.Errorf("ientrychecks request = %+v", seen[0])
	}

	verify := newTestClient(t, predictRecorder(t, 200, `{"diffs":{"r1":{"missingPredictedEntries":[]}},"score":{"precision":90,"recall":80,"f1":84.7},"scoresByProtocol":{"BGP":{"precision":90,"recall":80,"f1":84.7}}}`, &seen).URL).Predict
	v, _, err := verify.VerifyPredictModeling(ctx, "s1", false)
	if err != nil || v.Diffs["r1"] == nil || v.ScoresByProtocol["BGP"] == nil || v.Score == nil {
		t.Fatalf("verification = %+v, %v", v, err)
	}
	// The typed accessors decode Forward's PredictModelScore (percentages) from the raw parts, and a response without a score is not an error.
	typed := newTestClient(t, predictRecorder(t, 200, `{"diffs":{},"score":{"precision":97.5,"recall":88.25,"f1":92.64},"scoresByProtocol":{"BGP":{"precision":100.0,"recall":50.0,"f1":66.67}}}`, &seen).URL).Predict
	tv, _, err := typed.VerifyPredictModeling(ctx, "s1", false)
	if err != nil {
		t.Fatal(err)
	}
	if overall, ok, err := tv.OverallScore(); err != nil || !ok || overall != (PredictModelScore{Precision: 97.5, Recall: 88.25, F1: 92.64}) {
		t.Fatalf("OverallScore = %+v, %v, %v", overall, ok, err)
	}
	if byProto, err := tv.ProtocolScores(); err != nil || byProto["BGP"] != (PredictModelScore{Precision: 100, Recall: 50, F1: 66.67}) || len(byProto) != 1 {
		t.Fatalf("ProtocolScores = %+v, %v", byProto, err)
	}
	if _, ok, err := (&PredictModelVerification{}).OverallScore(); ok || err != nil {
		t.Fatalf("an empty verification reported a score: %v, %v", ok, err)
	}
	if _, ok, err := (&PredictModelVerification{Score: json.RawMessage(`"x"`)}).OverallScore(); ok || err == nil {
		t.Fatalf("a malformed score was accepted: %v, %v", ok, err)
	}
	if _, ok, err := (*PredictModelVerification)(nil).OverallScore(); ok || err != nil {
		t.Fatalf("a nil verification reported a score: %v, %v", ok, err)
	}
	if _, err := (&PredictModelVerification{ScoresByProtocol: map[string]json.RawMessage{"BGP": json.RawMessage(`"x"`)}}).ProtocolScores(); err == nil {
		t.Fatal("a malformed protocol score was accepted")
	}
	if _, _, err := verify.VerifyPredictModeling(ctx, "s1", false); err != nil {
		t.Fatal(err)
	}
	if got := seen[len(seen)-1].uri; got != "/api/internal/snapshots/s1/verify-predict-modeling?mode=base&returnDiffs=false" {
		t.Errorf("verify (no diffs) = %s", got)
	}
	if _, _, err := verify.VerifyPredictModeling(ctx, "s1", true); err != nil {
		t.Fatal(err)
	}
	if got := seen[len(seen)-1].uri; got != "/api/internal/snapshots/s1/verify-predict-modeling?mode=base" {
		t.Errorf("verify (diffs) = %s", got)
	}

	debug := newTestClient(t, predictRecorder(t, 200, `{"bgp":[]}`, &seen).URL).Predict
	if raw, _, err := debug.ProtocolSeedRoutes(ctx, "s1", ProtocolModelingQuery{Prefix: "10.0.0.0/8", Device: "r1", VRF: "default"}); err != nil || string(raw) != `{"bgp":[]}` {
		t.Fatalf("seed routes = %s, %v", raw, err)
	}
	if _, _, err := debug.ProtocolPathSelection(ctx, "s1", ProtocolModelingQuery{Prefix: "10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	before := len(seen)
	if _, _, err := debug.ProtocolSeedRoutes(ctx, "s1", ProtocolModelingQuery{Prefix: "10.0.0.0/8", VRF: "default"}); err == nil {
		t.Error("a VRF without a device was sent")
	}
	if _, _, err := debug.ProtocolPathSelection(ctx, "s1", ProtocolModelingQuery{}); err == nil || len(seen) != before {
		t.Error("a query without a prefix was sent")
	}
	if got := seen[before-2].uri; got != "/api/internal/snapshots/s1/protocol-modeling?deviceName=r1&prefix=10.0.0.0%2F8&view=seed-routes&vrfName=default" {
		t.Errorf("seed-routes = %s", got)
	}
	if got := seen[before-1].uri; got != "/api/internal/snapshots/s1/protocol-modeling?prefix=10.0.0.0%2F8&view=path-selection" {
		t.Errorf("path-selection = %s", got)
	}
}
