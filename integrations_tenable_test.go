package forward

import (
	"context"
	"testing"
)

// The shared scanner list holds Rapid7 and Tenable rows; the Tenable list keeps only its own. A network with none is an empty list, not nil.
func TestListTenableSCKeepsOnlyTenableRows(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	client := newTestClient(t, predictRecorder(t, 200, `[{"name":"r7","type":"RAPID7","baseUrl":"https://r7"},{"name":"sc","type":"TENABLE","baseUrl":"https://sc.example","credentialId":"H-5","maxPageSize":500,"collectorId":"c1","disableSslValidation":true}]`, &seen).URL)
	got, _, err := client.Integrations.ListTenableSC(context.Background(), "7")
	if err != nil || len(got) != 1 || got[0].Name != "sc" || got[0].CredentialID != "H-5" || *got[0].MaxPageSize != 500 || got[0].CollectorID != "c1" || !got[0].DisableSSLValidation {
		t.Fatalf("got %+v, %v", got, err)
	}
	if seen[0].method != "GET" || seen[0].uri != "/api/networks/7/end-host-scanners" {
		t.Fatalf("seen %+v", seen)
	}
	none := newTestClient(t, predictRecorder(t, 200, `[{"name":"r7","type":"RAPID7"}]`, &seen).URL)
	if got, _, err := none.Integrations.ListTenableSC(context.Background(), "7"); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("none = %#v, %v", got, err)
	}
}

// Wire shapes: create sends what Forward's NewTenableSource reads and omits an unset page size (so Forward's 10000 applies); a patch sends
// only what is set, with explicit nulls where the caller clears the page size or the collector; delete tolerates a source already gone;
// the test is the same route with action=test and a failure is a 200 carrying error.
func TestTenableSCWrites(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	client := newTestClient(t, predictRecorder(t, 200, `{"name":"sc","type":"TENABLE","baseUrl":"https://sc.example","credentialId":"H-5"}`, &seen).URL)
	ctx := context.Background()
	in := client.Integrations

	created, _, err := in.CreateTenableSC(ctx, "7", TenableSourceRequest{Name: "sc", BaseURL: "https://sc.example", CredentialID: "H-5", DisableSSLValidation: true})
	if err != nil || created.Name != "sc" {
		t.Fatalf("create = %+v, %v", created, err)
	}
	name, off := "sc2", true
	if _, _, err := in.UpdateTenableSC(ctx, "7", "sc", TenableSourcePatch{Name: &name, CollectionDisabled: &off, ClearMaxPageSize: true, ClearCollector: true}); err != nil {
		t.Fatal(err)
	}
	page := 250
	if _, _, err := in.UpdateTenableSC(ctx, "7", "sc2", TenableSourcePatch{MaxPageSize: &page}); err != nil {
		t.Fatal(err)
	}
	if _, err := in.DeleteTenableSC(ctx, "7", "sc2"); err != nil {
		t.Fatal(err)
	}
	want := []predictSeen{
		{"POST", "/api/networks/7/tenable-sources", `{"name":"sc","baseUrl":"https://sc.example","credentialId":"H-5","disableSslValidation":true}`, "application/json"},
		{"PATCH", "/api/networks/7/tenable-sources/sc", `{"collectionDisabled":true,"collectorId":null,"maxPageSize":null,"name":"sc2"}`, "application/json"},
		{"PATCH", "/api/networks/7/tenable-sources/sc2", `{"maxPageSize":250}`, "application/json"},
		{"DELETE", "/api/networks/7/tenable-sources/sc2", "", ""},
	}
	if len(seen) != len(want) {
		t.Fatalf("seen %+v", seen)
	}
	for i, w := range want {
		if seen[i].method != w.method || seen[i].uri != w.uri || seen[i].body != w.body || seen[i].contentType != w.contentType {
			t.Errorf("request %d = %+v, want %+v", i, seen[i], w)
		}
	}

	var seenOK []predictSeen
	okClient := newTestClient(t, predictRecorder(t, 200, `{}`, &seenOK).URL)
	if res, _, err := okClient.Integrations.TestTenableSC(ctx, "7", TenableSourceTest{Name: "sc", BaseURL: "https://sc.example", CredentialID: "H-5"}); err != nil || !res.OK() {
		t.Fatalf("a passing test = %+v, %v", res, err)
	}

	var seenGone []predictSeen
	gone := newTestClient(t, predictRecorder(t, 404, `{"message":"No such source"}`, &seenGone).URL)
	if _, err := gone.Integrations.DeleteTenableSC(ctx, "7", "x"); err != nil {
		t.Errorf("a missing source was an error: %v", err)
	}

	var seenTest []predictSeen
	tester := newTestClient(t, predictRecorder(t, 200, `{"error":"401 from Security Center"}`, &seenTest).URL)
	res, _, err := tester.Integrations.TestTenableSC(ctx, "7", TenableSourceTest{Name: "sc", BaseURL: "https://sc.example", CredentialID: "H-5", CollectorID: "c1"})
	if err != nil || res.OK() || res.Error == "" {
		t.Fatalf("test = %+v, %v", res, err)
	}
	if seenTest[0].uri != "/api/networks/7/tenable-sources?action=test" || seenTest[0].body != `{"name":"sc","baseUrl":"https://sc.example","credentialId":"H-5","collectorId":"c1"}` {
		t.Fatalf("test request %+v", seenTest[0])
	}
}

// Forward answers 400 for these, so none reaches the wire.
func TestTenableSCRefusals(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	in := newTestClient(t, predictRecorder(t, 200, `{}`, &seen).URL).Integrations
	ctx := context.Background()
	zero := 0
	for name, err := range map[string]error{
		"create no credential": func() error {
			_, _, err := in.CreateTenableSC(ctx, "7", TenableSourceRequest{Name: "a", BaseURL: "https://x"})
			return err
		}(),
		"create no name": func() error {
			_, _, err := in.CreateTenableSC(ctx, "7", TenableSourceRequest{BaseURL: "https://x", CredentialID: "1"})
			return err
		}(),
		"create zero page": func() error {
			_, _, err := in.CreateTenableSC(ctx, "7", TenableSourceRequest{Name: "a", BaseURL: "https://x", CredentialID: "1", MaxPageSize: &zero})
			return err
		}(),
		"create spaced name": func() error {
			_, _, err := in.CreateTenableSC(ctx, "7", TenableSourceRequest{Name: "my sc", BaseURL: "https://x", CredentialID: "H-1"})
			return err
		}(),
		"update spaced name": func() error { _, _, err := in.UpdateTenableSC(ctx, "7", "a b", TenableSourcePatch{}); return err }(),
		"update zero page": func() error {
			_, _, err := in.UpdateTenableSC(ctx, "7", "a", TenableSourcePatch{MaxPageSize: &zero})
			return err
		}(),
		"update no name": func() error { _, _, err := in.UpdateTenableSC(ctx, "7", " ", TenableSourcePatch{}); return err }(),
		"delete no name": func() error { _, err := in.DeleteTenableSC(ctx, "7", ""); return err }(),
		"test no url": func() error {
			_, _, err := in.TestTenableSC(ctx, "7", TenableSourceTest{Name: "a", CredentialID: "1"})
			return err
		}(),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if len(seen) != 0 {
		t.Fatalf("a refused call reached the wire: %+v", seen)
	}
}
