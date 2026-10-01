package forward

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The shapes are the published CLI/SNMP/HTTP endpoint-profile definitions; a
// profile's ID carries its type, and a field this SDK does not model survives
// in Raw.
func TestEndpointProfilesReadAllThreeTypes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/endpoint-profiles":
			_, _ = io.WriteString(w, `{"profiles":[
			  {"id":"CLI-7","type":"CLI","name":"linux","detectorCommand":"uname -a","detectorPatterns":["Linux"],"prompt":"\\$ $","ptyType":"vt100","ptyColumnSize":200,
			   "responseTimeoutSec":30,"commandSets":["UNIX"],"customCommands":["ip route"],"createdBy":"mary","futureField":{"x":1}},
			  {"id":"SNMP-5","type":"SNMP","name":"printer","detectorOid":"1.3.6.1.2.1.1.2.0","detectorPatterns":["^1.3.6.1.4.1.11"],"oidSets":["STANDARD"],
			   "customOids":[{"name":"system_desc","oid":"1.3.6.1.2.1.1.1"}]},
			  {"id":"HTTP-4","type":"HTTP","name":"nsx","https":true,"authType":"BASIC_AUTH","headers":{"Accept":"application/json"},"detectorUri":"/api/v1/node",
			   "endpoints":[{"name":"compute_managers","path":"/api/v1/fabric/compute-managers","paginationModel":{"type":"OFFSET","parameterName":"page"}}]}]}`)
		case "/api/endpoint-profiles/CLI-404":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"No endpoint profile CLI-404"}`)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	profiles, _, err := client.Endpoints.ListProfiles(context.Background(), "")
	if err != nil || len(profiles) != 3 {
		t.Fatalf("ListProfiles() = %d, %v", len(profiles), err)
	}
	cli, snmp, web := profiles[0], profiles[1], profiles[2]
	if cli.ID != "CLI-7" || cli.DetectorCommand != "uname -a" || *cli.PtyColumnSize != 200 || *cli.ResponseTimeoutSec != 30 || cli.CustomCommands[0] != "ip route" || cli.CreatedBy != "mary" {
		t.Fatalf("CLI = %+v", cli)
	}
	if _, ok := cli.Raw["futureField"]; !ok {
		t.Fatal("an unmodelled field must survive in Raw")
	}
	if snmp.DetectorOID != "1.3.6.1.2.1.1.2.0" || len(snmp.CustomOIDs) != 1 || snmp.CustomOIDs[0].Name != "system_desc" || snmp.OIDSets[0] != "STANDARD" {
		t.Fatalf("SNMP = %+v", snmp)
	}
	if web.HTTPS == nil || !*web.HTTPS || web.AuthType != "BASIC_AUTH" || web.Headers["Accept"] != "application/json" ||
		len(web.Endpoints) != 1 || !bytes.Contains(web.Endpoints[0].PaginationModel, []byte(`"OFFSET"`)) {
		t.Fatalf("HTTP = %+v", web)
	}
	if got, _, err := client.Endpoints.GetProfile(context.Background(), "CLI-404"); got != nil || err != nil {
		t.Fatalf("absent GetProfile() = %+v, %v; want nil, nil", got, err)
	}
}

// With no list uploaded Forward returns its defaults, unsigned and
// unattributed; an uploaded list carries the uploaded* attribution.
func TestEndpointsApprovedCLICommands(t *testing.T) {
	t.Parallel()

	for body, uploaded := range map[string]bool{
		`{"commands":["show version","show running-config"]}`: false,
		`{"commands":["show version"],"signedAt":"2026-09-01T00:00:00Z","uploadedAt":"2026-09-02T00:00:00Z","uploadedBy":"mary","uploadedById":"12"}`: true,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.EscapedPath() != "/api/approved-cli-commands" {
				t.Errorf("path = %s", r.URL.EscapedPath())
			}
			_, _ = io.WriteString(w, body)
		}))
		got, _, err := newTestClient(t, server.URL).Endpoints.ApprovedCLICommands(context.Background())
		server.Close()
		if err != nil || len(got.Commands) == 0 || (got.UploadedBy != "") != uploaded || (got.SignedAt != "") != uploaded {
			t.Errorf("%s: %+v, %v", body, got, err)
		}
	}
}

// Spring answers 406 when the handler's error is JSON and the request did not
// accept JSON -- which hid every real error behind "Not Acceptable". The text
// reads accept JSON too, so the 404 (and its message) comes through.
func TestTextReadsAcceptJSONErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Device collection id map not found for snapshot 9164"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	var buf bytes.Buffer
	if _, _, _, err := client.Snapshots.Logs(context.Background(), "9164", SnapshotLogOptions{}, 1024, &buf); !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("Logs error = %v, want the real 404", err)
	}
	if _, err := client.Devices.DownloadFile(context.Background(), "n1", "leaf-1", "CONFIGURATION.txt", "", &buf); !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("DownloadFile error = %v, want the real 404", err)
	}
}
