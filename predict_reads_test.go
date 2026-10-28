package forward

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The OS list and the allowed commands are not network-scoped; an unknown protocol never reaches the wire.
func TestPredictCatalogReads(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, predictSeen{r.Method, r.URL.RequestURI(), "", r.Header.Get("Accept")})
		if strings.HasSuffix(r.URL.Path, "/allowed-commands") {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "router bgp <asn>\n  neighbor <ip> remote-as <asn>\n")
			return
		}
		_, _ = io.WriteString(w, `["junos","arista_eos"]`)
	}))
	defer server.Close()
	predict := newTestClient(t, server.URL).Predict
	ctx := context.Background()

	oses, _, err := predict.PredictOSes(ctx)
	if err != nil || strings.Join(oses, ",") != "arista_eos,junos" {
		t.Fatalf("PredictOSes = %v, %v", oses, err)
	}
	text, _, err := predict.AllowedCommands(ctx, "arista_eos", PredictProtocolBGP)
	if err != nil || !strings.Contains(text, "neighbor <ip>") {
		t.Fatalf("AllowedCommands = %q, %v", text, err)
	}
	if seen[0].uri != "/api/predict-oses" || seen[1].uri != "/api/predict-oses/arista_eos/allowed-commands?protocol=BGP" || !strings.Contains(seen[1].contentType, "text/plain") {
		t.Fatalf("seen = %+v", seen)
	}
	if _, _, err := predict.AllowedCommands(ctx, "arista_eos", "RSVP"); err == nil {
		t.Fatal("an unknown protocol was sent")
	}
	if _, _, err := predict.AllowedCommands(ctx, " ", PredictProtocolBGP); err == nil || len(seen) != 2 {
		t.Fatal("a blank OS was sent")
	}
}

// Firewall objects: one route per kind under the change set's device, raw body back; a read refused for the permission is the typed 403.
func TestFirewallObjectsAndPrettifyAndExport(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, predictSeen{r.Method, r.URL.RequestURI(), string(b), r.Header.Get("Content-Type")})
		switch {
		case strings.HasSuffix(r.URL.Path, "/zones"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"Missing permission: NetworkOperation.VIEW_CHANGE_SET_SECURITY_DATA"}`)
		case strings.HasSuffix(r.URL.RequestURI(), "action=prettify"):
			_, _ = io.WriteString(w, `{"prettified":"interface Ethernet1\n  shutdown\n"}`)
		case r.URL.RawQuery == "format=zip":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = io.WriteString(w, "PK\x03\x04zipbytes")
		default:
			_, _ = io.WriteString(w, `{"objects":[{"name":"web","ips":["10.0.0.1"]}]}`)
		}
	}))
	defer server.Close()
	predict := newTestClient(t, server.URL).Predict
	ctx := context.Background()

	raw, _, err := predict.FirewallObjects(ctx, "7", "3", "fw 1", FirewallAddressObjects)
	if err != nil || !strings.Contains(string(raw), `"web"`) || seen[0].uri != "/api/networks/7/change-sets/3/devices/fw%201/address-objects" {
		t.Fatalf("FirewallObjects = %s, %v, seen %+v", raw, err, seen)
	}
	_, _, err = predict.FirewallObjects(ctx, "7", "3", "fw1", FirewallZones)
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
	if _, _, err := predict.FirewallObjects(ctx, "7", "3", "fw1", "rules"); err == nil {
		t.Fatal("an unknown kind was sent")
	}
	pretty, _, err := predict.PrettifyCommands(ctx, "7", "3", "sw1", "interface Ethernet1\nshutdown")
	if err != nil || pretty != "interface Ethernet1\n  shutdown\n" {
		t.Fatalf("PrettifyCommands = %q, %v", pretty, err)
	}
	last := seen[len(seen)-1]
	if last.method != "POST" || last.uri != "/api/networks/7/change-sets/3/devices/sw1/commands?action=prettify" || last.body != "interface Ethernet1\nshutdown" || !strings.HasPrefix(last.contentType, "text/plain") {
		t.Fatalf("prettify request = %+v", last)
	}
	var zip bytes.Buffer
	if _, err := predict.ExportChangeSetZip(ctx, "7", "3", &zip); err != nil || zip.String() != "PK\x03\x04zipbytes" {
		t.Fatalf("ExportChangeSetZip = %q, %v", zip.String(), err)
	}
}

// BGP diffs come back as an ordinary JSON array (Forward decompresses its own blob); a device with no BGP changes is an empty list.
func TestBGPAdvertisementDiffs(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, predictSeen{r.Method, r.URL.RequestURI(), "", ""})
		if strings.Contains(r.URL.Path, "/commits/") {
			_, _ = io.WriteString(w, `[]`)
			return
		}
		_, _ = io.WriteString(w, `[{"change":"MODIFIED","original":{"prefix":"10.0.0.0/24"},"updateAdded":{"prefix":"10.0.0.0/24","med":5}},{"change":"WITHDRAWN","updateRemoved":{"prefix":"10.1.0.0/24"}}]`)
	}))
	defer server.Close()
	predict := newTestClient(t, server.URL).Predict
	ctx := context.Background()

	diffs, _, err := predict.BGPAdvertisementDiffs(ctx, "7", "3", "edge1")
	if err != nil || len(diffs) != 2 || diffs[0].Change != "MODIFIED" || !strings.Contains(string(diffs[0].UpdateAdded), `"med":5`) || diffs[1].Change != "WITHDRAWN" || diffs[1].UpdateAdded != nil {
		t.Fatalf("diffs = %+v, %v", diffs, err)
	}
	committed, _, err := predict.CommittedBGPAdvertisementDiffs(ctx, "7", "3", "c9", "edge1")
	if err != nil || committed == nil || len(committed) != 0 {
		t.Fatalf("committed = %#v, %v; want an empty non-nil list", committed, err)
	}
	if seen[0].uri != "/api/networks/7/change-sets/3/draft/devices/edge1/bgp-advertisements?view=diffs" ||
		seen[1].uri != "/api/networks/7/change-sets/3/commits/c9/devices/edge1/bgp-advertisements?view=diffs" {
		t.Fatalf("seen = %+v", seen)
	}
}

// Protocol modeling status is {"done": bool}; the affected-devices list is a plain array, except that a base snapshot gets Forward's
// {"error": ...} payload, which must not read as an empty list of affected devices.
func TestProtocolModelingAndAffectedDevices(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, predictSeen{r.Method, r.URL.RequestURI(), "", ""})
		switch {
		case strings.Contains(r.URL.Path, "/s-base/"):
			_, _ = io.WriteString(w, `{"error":"No affected devices since this is a base snapshot"}`)
		case r.URL.RawQuery == "view=status":
			_, _ = io.WriteString(w, `{"done":true}`)
		case strings.Contains(r.URL.Path, "/s-empty/"):
			_, _ = io.WriteString(w, `[]`)
		default:
			_, _ = io.WriteString(w, `["edge1","core2"]`)
		}
	}))
	defer server.Close()
	predict := newTestClient(t, server.URL).Predict
	ctx := context.Background()

	done, _, err := predict.ProtocolModelingDone(ctx, "s9")
	if err != nil || !done {
		t.Fatalf("done = %v, %v", done, err)
	}
	names, _, err := predict.AffectedDevices(ctx, "s9")
	if err != nil || strings.Join(names, ",") != "edge1,core2" {
		t.Fatalf("names = %v, %v", names, err)
	}
	empty, _, err := predict.AffectedDevices(ctx, "s-empty")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %#v, %v", empty, err)
	}
	if _, _, err := predict.AffectedDevices(ctx, "s-base"); !errors.Is(err, ErrNoAffectedDevices) {
		t.Fatalf("a base snapshot gave %v, want ErrNoAffectedDevices", err)
	}
	if seen[0].uri != "/api/internal/snapshots/s9/protocol-modeling?view=status" || seen[1].uri != "/api/internal/snapshots/s9/protocol-modeling?view=affected-devices" {
		t.Fatalf("seen = %+v", seen)
	}
	if _, _, err := predict.AffectedDevices(ctx, " "); err == nil {
		t.Fatal("a blank snapshot id was sent")
	}
}

// A change set's NQE checks come from the library route with both ids as query parameters; none is an empty list.
func TestChangeSetNQEChecks(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, predictSeen{r.Method, r.URL.RequestURI(), "", ""})
		if strings.Contains(r.URL.RawQuery, "changeSetId=empty") {
			_, _ = io.WriteString(w, `[]`)
			return
		}
		_, _ = io.WriteString(w, `[{"queryId":"Q_1","presentInHead":true,"repository":"ORG","lastCommitSourceCodeSha":"abc","commits":[{"id":"c1","committedAt":"2026-10-01T00:00:00Z","path":"/checks/a","intent":"no leaks","hasParams":true}]}]`)
	}))
	defer server.Close()
	predict := newTestClient(t, server.URL).Predict
	ctx := context.Background()

	checks, _, err := predict.ChangeSetNQEChecks(ctx, "7", "3")
	if err != nil || len(checks) != 1 || checks[0].QueryID != "Q_1" || !checks[0].PresentInHead || checks[0].Restricted || checks[0].Commits[0].Path != "/checks/a" || !checks[0].Commits[0].HasParams {
		t.Fatalf("checks = %+v, %v", checks, err)
	}
	none, _, err := predict.ChangeSetNQEChecks(ctx, "7", "empty")
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("none = %#v, %v", none, err)
	}
	if seen[0].uri != "/api/nqe/verificationQueries?changeSetId=3&networkId=7" {
		t.Fatalf("uri = %q", seen[0].uri)
	}
}
