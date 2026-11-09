package forward

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// Predict's internal debug reads (BenchmarkController) are keyed by query
// parameter on shared paths, so each must send exactly the parameter Forward's
// mapping selects on: bgp-nodes and ospf-nodes need a present "devices"
// parameter, the subset read needs view=changeSetSubset and repeated
// changedDevices, and the node reads carry device and VRF names as escaped path
// segments. A bare path would hit a different handler or none.
func TestPredictInternalReadsSendTheParametersForwardSelectsOn(t *testing.T) {
	t.Parallel()

	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI()+" accept="+r.Header.Get("Accept"))
		switch {
		case r.URL.Query().Get("view") == "changeSetSubset":
			_, _ = io.WriteString(w, `{"names":["a","b"]}`)
		case r.Header.Get("Accept") == "application/zip":
			_, _ = io.WriteString(w, "PKzip")
		default:
			_, _ = io.WriteString(w, `{"ok":true}`)
		}
	}))
	defer server.Close()
	p := newTestClient(t, server.URL).Predict
	ctx := context.Background()
	three, threshold := 3, 50

	steps := []struct {
		want string
		do   func() error
	}{
		{"GET /api/internal/snapshots/s1/protocol-modeling?view=partitions accept=application/json", func() error { _, _, err := p.ProtocolModelingPartitions(ctx, "s1"); return err }},
		{"GET /api/internal/snapshots/s1/bgp-nodes?devices= accept=application/json", func() error { _, _, err := p.BGPNodeDevices(ctx, "s1"); return err }},
		{"GET /api/internal/snapshots/s1/ospf-nodes?devices= accept=application/json", func() error { _, _, err := p.OSPFNodeDevices(ctx, "s1"); return err }},
		{"GET /api/internal/snapshots/s1/devices/r%201/vrfs/mgmt/bgp-node accept=application/json", func() error { _, _, err := p.BGPNode(ctx, "s1", "r 1", "mgmt"); return err }},
		{"GET /api/internal/snapshots/s1/devices/r1/ospf-nodes accept=application/json", func() error { _, _, err := p.OSPFNodes(ctx, "s1", "r1", ""); return err }},
		{"GET /api/internal/snapshots/s1/devices/r1/ospf-nodes?vrfName=mgmt accept=application/json", func() error { _, _, err := p.OSPFNodes(ctx, "s1", "r1", "mgmt"); return err }},
		{"GET /api/internal/snapshots/s1/bgp-rules?deviceName=r1&vrfName=default accept=application/json", func() error { _, _, err := p.BGPRules(ctx, "s1", "r1", "default", ""); return err }},
		{"GET /api/internal/snapshots/s1/bgp-rules?deviceName=r1&neighborAddr=10.0.0.2&vrfName=default accept=application/json", func() error { _, _, err := p.BGPRules(ctx, "s1", "r1", "default", "10.0.0.2"); return err }},
	}
	for _, step := range steps {
		got = nil
		if err := step.do(); err != nil {
			t.Fatalf("%s: %v", step.want, err)
		}
		if len(got) != 1 || got[0] != step.want {
			t.Errorf("sent %v, want %q", got, step.want)
		}
	}

	got = nil
	names, _, err := p.ChangeSetSubset(ctx, "s1", []string{"r1", " r2 ", ""}, ChangeSetSubsetOptions{NumIterations: &three, Threshold: &threshold})
	if err != nil || !reflect.DeepEqual(names, []string{"a", "b"}) {
		t.Fatalf("ChangeSetSubset = %v, %v", names, err)
	}
	if want := "GET /api/internal/snapshots/s1/rp-nodes?changedDevices=r1&changedDevices=r2&numIterations=3&threshold=50&view=changeSetSubset accept=application/json"; len(got) != 1 || got[0] != want {
		t.Errorf("subset sent %v, want %q", got, want)
	}

	got = nil
	var zip bytes.Buffer
	if _, err := p.DownloadProtocolModeling(ctx, "s1", &zip); err != nil || zip.String() != "PKzip" {
		t.Fatalf("DownloadProtocolModeling = %q, %v", zip.String(), err)
	}
	if want := "GET /api/internal/snapshots/s1/protocol-modeling accept=application/zip"; len(got) != 1 || got[0] != want {
		t.Errorf("download sent %v, want %q", got, want)
	}
}

// Input Forward would refuse is refused before sending, so a bad call never
// reaches a debug route.
func TestPredictInternalReadsRefuseBadInputWithoutSending(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	p := newTestClient(t, server.URL).Predict
	ctx := context.Background()
	zero, neg := 0, -1

	for name, err := range map[string]error{
		"no snapshot":     func() error { _, _, err := p.BGPNodeDevices(ctx, " "); return err }(),
		"node no vrf":     func() error { _, _, err := p.BGPNode(ctx, "s1", "r1", ""); return err }(),
		"rules no device": func() error { _, _, err := p.BGPRules(ctx, "s1", "", "default", ""); return err }(),
		"ospf no device":  func() error { _, _, err := p.OSPFNodes(ctx, "s1", "", ""); return err }(),
		"subset no device": func() error {
			_, _, err := p.ChangeSetSubset(ctx, "s1", []string{" "}, ChangeSetSubsetOptions{})
			return err
		}(),
		"subset negative": func() error {
			_, _, err := p.ChangeSetSubset(ctx, "s1", []string{"r1"}, ChangeSetSubsetOptions{NumIterations: &neg})
			return err
		}(),
		"subset threshold": func() error {
			_, _, err := p.ChangeSetSubset(ctx, "s1", []string{"r1"}, ChangeSetSubsetOptions{Threshold: &zero})
			return err
		}(),
		"download no dst": func() error { _, err := p.DownloadProtocolModeling(ctx, "s1", nil); return err }(),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if calls != 0 {
		t.Fatalf("%d refused calls reached the server", calls)
	}
}

// A refusal on the ZIP route is still a typed error, and the long wait applies to the protocol-modeling reads on a client whose own
// timeout is short.
func TestPredictProtocolModelingReadsKeepTypedErrorsAndOutwaitShortClients(t *testing.T) {
	t.Parallel()

	var fail bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"apiUrl":"/api/internal/snapshots/s1/protocol-modeling","message":"no"}`)
			return
		}
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, `{"partitions":[]}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{
		BaseURL:    server.URL,
		Username:   "user",
		Password:   "pass",
		HTTPClient: &http.Client{Transport: privateHTTPClient().Transport, Timeout: 50 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err := client.Predict.ProtocolModelingPartitions(ctx, "s1"); err != nil {
		t.Fatalf("partitions did not outwait a short client timeout: %v", err)
	}
	fail = true
	if _, err := client.Predict.DownloadProtocolModeling(ctx, "s1", io.Discard); !IsStatus(err, http.StatusForbidden) {
		t.Fatalf("a refused download = %v, want a typed 403", err)
	}
}
