package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSegmentsIgnoreVariableNamesAndPatterns(t *testing.T) {
	t.Parallel()

	for route, want := range map[string]string{
		"/api/networks/{networkId}/aliases":         "api|networks|{}|aliases",
		"/api/endpoint-profiles/{profileId:CLI-.*}": "api|endpoint-profiles|{}",
		"/api/x/{id:[0-9]{2}}/y":                    "api|x|{}|y",
		"/api/users/{userId}/roles/org/ADMIN/":      "api|users|{}|roles|org|ADMIN",
		"//api//a":                                  "api|a",
		"/api{?PagePaths.APP}":                      "api{}",
	} {
		if got := strings.Join(segments(route), "|"); got != want {
			t.Errorf("segments(%q) = %q, want %q", route, got, want)
		}
	}
}

// The three differences between the manifest's spelling and Forward's mappings that the survey found, and the case that
// must NOT match: a mapped literal alone does not prove the manifest's variable route exists.
func TestMatchesRules(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		method, manifest string
		mapped           Route
		want             bool
	}{
		{"regex segment", "PATCH", "/api/endpoint-profiles/{profileId}", Route{Method: "PATCH", Path: "/api/endpoint-profiles/{profileId:CLI-.*}"}, true},
		{"manifest literal, mapped variable", "DELETE", "/api/users/{userId}/roles/org/ADMIN", Route{Method: "DELETE", Path: "/api/users/{userId}/roles/org/{role}"}, true},
		{"different variable names", "POST", "/api/access-control-groups/{groupId}/network-roles/{networkId}", Route{Method: "POST", Path: "/api/access-control-groups/{id}/network-roles/{networkId}"}, true},
		{"manifest variable, mapped literal only", "GET", "/api/users/{userId}", Route{Method: "GET", Path: "/api/users/current"}, false},
		{"different literal", "GET", "/api/users/current", Route{Method: "GET", Path: "/api/users/me"}, false},
		{"different method", "GET", "/api/networks", Route{Method: "POST", Path: "/api/networks"}, false},
		{"ANY serves every method", "PUT", "/api/networks", Route{Method: "ANY", Path: "/api/networks"}, true},
		{"different depth", "GET", "/api/networks/{networkId}", Route{Method: "GET", Path: "/api/networks/{networkId}/snapshots"}, false},
		{"method case", "get", "/api/networks", Route{Method: "GET", Path: "/api/networks"}, true},
	}
	for _, tc := range cases {
		if got := matches(tc.method, tc.manifest, tc.mapped); got != tc.want {
			t.Errorf("%s: matches(%s %s, %s %s) = %v, want %v", tc.name, tc.method, tc.manifest, tc.mapped.Method, tc.mapped.Path, got, tc.want)
		}
	}
}

func TestParseAllow(t *testing.T) {
	t.Parallel()

	entries, err := parseAllow("# header\n\nGET /api/a  # served only by a newer build\nPOST /api/b pin=stable,primary # not in these\n")
	if err != nil || len(entries) != 2 {
		t.Fatalf("parseAllow = %+v, %v", entries, err)
	}
	if !entries[0].covers("get", "/api/a", "stable") || entries[0].covers("GET", "/api/other", "stable") {
		t.Error("an entry without pins covers every pin, exactly its own route")
	}
	if !entries[1].covers("POST", "/api/b", "stable") || entries[1].covers("POST", "/api/b", "beta") {
		t.Error("an entry with pins covers only those pins")
	}
	for name, text := range map[string]string{
		"no reason":      "GET /api/a\n",
		"empty reason":   "GET /api/a #   \n",
		"bad pin field":  "GET /api/a stable # x\n",
		"too few fields": "GET # x\n",
		"too many":       "GET /api/a pin=x extra # y\n",
	} {
		if _, err := parseAllow(text); err == nil {
			t.Errorf("%s must be refused: an exception nobody can explain is a route that was never checked", name)
		}
	}
}

func TestCheckReport(t *testing.T) {
	t.Parallel()

	mapped := map[string][]Route{
		"primary": {{Method: "GET", Path: "/api/a"}, {Method: "GET", Path: "/api/gated", Profile: "Profile(Profiles.SAAS)"}, {Method: "GET", Path: "/api/mixed", Profile: "Profile(Profiles.SAAS)"}, {Method: "GET", Path: "/api/mixed"}},
		"stable":  {{Method: "GET", Path: "/api/a"}},
	}
	endpoints := []Endpoint{
		{Method: "GET", Route: "/api/a"},
		{Method: "GET", Route: "/api/gone", Symbols: []string{"X.Gone"}},
		{Method: "GET", Route: "/api/gated"},
		{Method: "GET", Route: "/api/mixed"},
		{Method: "GET", Route: "/api/stable-lacks"},
	}
	allow := []allowEntry{
		{method: "GET", route: "/api/stable-lacks", pins: []string{"stable"}, reason: "newer"},
		{method: "GET", route: "/api/unused", reason: "stale"},
	}
	report := check(endpoints, mapped, allow)

	var missing []string
	for _, f := range report.Missing {
		missing = append(missing, f.Pin+" "+f.Route)
	}
	// gone: missing at both pins. gated: stable lacks it. mixed: stable lacks it. stable-lacks: primary lacks it
	// (the allow entry covers stable only), and stable is allowed.
	wantMissing := "primary /api/gone|stable /api/gone|stable /api/gated|stable /api/mixed|primary /api/stable-lacks"
	if got := strings.Join(missing, "|"); got != wantMissing {
		t.Errorf("missing = %q, want %q", got, wantMissing)
	}
	if len(report.Allowed) != 1 || report.Allowed[0].Pin != "stable" || report.Allowed[0].Reason != "newer" {
		t.Errorf("allowed = %+v", report.Allowed)
	}
	if len(report.Conditions) != 1 || report.Conditions[0].Route != "/api/gated" || report.Conditions[0].Pin != "primary" {
		t.Errorf("a route is conditional only when EVERY mapping is profile-gated; got %+v", report.Conditions)
	}
	if len(report.Unused) != 1 || report.Unused[0].route != "/api/unused" {
		t.Errorf("unused = %+v", report.Unused)
	}
}

// End to end against a real Forward checkout: a manifest route no controller maps must fail the run and be named, and
// an allow entry with a reason must let it through. Skips without the checkout.
func TestRunFailsOnAnUnmappedRoute(t *testing.T) {
	fwd := os.Getenv("FWD_SRC")
	if fwd == "" {
		fwd = filepath.Join(os.Getenv("HOME"), "src", "fwd")
	}
	pins, err := readPins("../../scripts/forward-pins")
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", fwd, "rev-parse", "--verify", pins[0].commit+"^{commit}").Run(); err != nil {
		t.Skipf("no Forward checkout with %s at %s", pins[0].commit, fwd)
	}
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	body := `{"endpoints":[{"method":"GET","route":"/api/networks/{networkId}/snapshots","symbols":["Snapshots.List"]},` +
		`{"method":"GET","route":"/api/no-such-route-anywhere","symbols":["Fake.Method"]}]}`
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pinFile := filepath.Join(dir, "pins")
	if err := os.WriteFile(pinFile, []byte("primary="+pins[0].commit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = run(fwd, pinFile, manifest, filepath.Join(dir, "no-allow-file"))
	if err == nil || !strings.Contains(err.Error(), "1 manifest route(s) are not mapped") {
		t.Fatalf("an unmapped manifest route must fail the check, got %v", err)
	}
	allow := filepath.Join(dir, "allow.txt")
	if err := os.WriteFile(allow, []byte("GET /api/no-such-route-anywhere  # deliberately absent in this test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(fwd, pinFile, manifest, allow); err != nil {
		t.Fatalf("an allow entry with a reason must let it through: %v", err)
	}
}
