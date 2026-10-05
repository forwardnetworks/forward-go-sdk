package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func routeKeys(routes []Route, withSource bool) []string {
	keys := make([]string, 0, len(routes))
	for _, r := range routes {
		key := r.Method + "\t" + r.Path + "\t" + r.Params
		if withSource {
			key += "\t" + r.Source
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

const fixtureController = `package com.forwardnetworks.cv.web.controller;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping(produces = MimeType.JSON)
@RequiredArgsConstructor
@ProfileCriteria(not = Profiles.ON_PREM)
public class FixtureController {
    private static final String BASE = "/widgets";
    private static final String ONE = BASE + "/{widgetId}";

    @GetMapping(BASE)
    public List<Widget> list() { return null; }

    @GetMapping(path = ONE, params = "view=full")
    public Widget full() { return null; }

    @PostMapping(path = { "/widgets", "/gadgets" }, consumes = MimeType.JSON)
    public Widget create(@RequestBody Widget w) { return null; }

    @DeleteMapping("/widgets/{widgetId:W-.*}")
    @Deprecated
    public void remove(@PathVariable("widgetId") String id) { }

    @RequestMapping(path = "/legacy", method = { RequestMethod.GET, RequestMethod.HEAD })
    public String legacy() { return ""; }

    @GetMapping(ServletPaths.API + "/direct")
    public String direct() { return ""; }

    @GetMapping(OTHER.PAGE)
    public String unresolved() { return ""; }
}
`

func TestControllerRoutesFromFixture(t *testing.T) {
	t.Parallel()

	routes := controllerRoutes("java/Fixture.java", fixtureController)
	got := map[string]Route{}
	for _, r := range routes {
		got[r.Method+" "+r.Path] = r
	}
	for _, want := range []string{
		"GET /api/widgets",
		"GET /api/widgets/{widgetId}",
		"POST /api/widgets",
		"POST /api/gadgets",
		"DELETE /api/widgets/{widgetId:W-.*}",
		"GET /api/legacy",
		"HEAD /api/legacy",
		"GET /api/api/direct",
		"GET /api{?OTHER.PAGE}",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing route %q; got %v", want, routeKeys(routes, false))
		}
	}
	if len(routes) != 9 {
		t.Errorf("got %d routes, want 9: %v", len(routes), routeKeys(routes, false))
	}
	if r := got["GET /api/widgets/{widgetId}"]; r.Params != `"view=full"` {
		t.Errorf("params = %q", r.Params)
	}
	if !got["DELETE /api/widgets/{widgetId:W-.*}"].Deprecated || got["GET /api/widgets"].Deprecated {
		t.Error("@Deprecated must mark only the method it annotates")
	}
	if p := got["GET /api/widgets"].Profile; p != "ProfileCriteria(not = Profiles.ON_PREM)" {
		t.Errorf("profile = %q", p)
	}
	if controllerRoutes("java/Plain.java", "public class Plain { @GetMapping(\"/x\") void x() {} }") != nil {
		t.Error("a class that is not a controller must yield no routes")
	}
}

const fixtureSecurity = `
public class SecurityConfig {
    static final String LOGIN = "/login";
    static final String IMPERSONATE = "/api/admin/impersonate";
    static final String UNIMPERSONATE = "/api/unimpersonate";
    void configure() {
        http.formLogin(f -> f.loginPage(LOGIN).loginProcessingUrl(PagePaths.LOGIN_PROCESS));
        filter.setSwitchUserMatcher(MATCHERS.matcher(HttpMethod.GET, IMPERSONATE));
        filter.setExitUserMatcher(MATCHERS.matcher(HttpMethod.GET, UNIMPERSONATE));
    }
}
`

func TestSecurityRoutesFailClosed(t *testing.T) {
	t.Parallel()

	sources := map[string]string{
		securityConfigFile: fixtureSecurity,
		pagePathsFile:      `public final class PagePaths { public static final String LOGIN_PROCESS = "/login"; }`,
		samlConfigFile:     "matcher(Saml2AuthenticationRequestResolver.DEFAULT_AUTHENTICATION_REQUEST_URI) .loginProcessingUrl(Saml2WebSsoAuthenticationFilter.DEFAULT_FILTER_PROCESSES_URI)",
	}
	routes, err := securityRoutes(sources)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(routeKeys(routes, false), "|")
	for _, want := range []string{"GET\t/login\t", "POST\t/login\t", "GET\t/api/admin/impersonate\t", "GET\t/api/unimpersonate\t", "GET\t/saml2/authenticate/{registrationId}\t", "POST\t/login/saml2/sso/{registrationId}\t"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	// A refactor that changes the shape must fail loudly rather than drop the routes from the table.
	broken := map[string]string{securityConfigFile: "public class SecurityConfig {}", pagePathsFile: "", samlConfigFile: sources[samlConfigFile]}
	if _, err := securityRoutes(broken); err == nil || !strings.Contains(err.Error(), "changed shape") {
		t.Errorf("a security config without the matchers must be an error, got %v", err)
	}
	noSAML := map[string]string{securityConfigFile: fixtureSecurity, pagePathsFile: sources[pagePathsFile], samlConfigFile: "nothing"}
	if _, err := securityRoutes(noSAML); err == nil {
		t.Error("a SAML config without its matchers must be an error")
	}
}

// TestExtractMatchesSkyforgeTables holds the Go port to the Python tools it was ported from: at the same Forward commits
// the two must find exactly the same routes. It needs Forward's source and Skyforge's generated tables, so it skips
// without them (the tables are not tracked in Skyforge's repository, and the check itself does not depend on them).
func TestExtractMatchesSkyforgeTables(t *testing.T) {
	fwd := os.Getenv("FWD_SRC")
	if fwd == "" {
		fwd = filepath.Join(os.Getenv("HOME"), "src", "fwd")
	}
	tables := os.Getenv("SKYFORGE_ROUTE_TABLES")
	if tables == "" {
		tables = filepath.Join(os.Getenv("HOME"), "src", "skyforge", "components", "server", "internal", "forwardrawgate", "routes")
	}
	pins, err := readPins("../../scripts/forward-pins")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pins {
		table, err := os.ReadFile(filepath.Join(tables, "routes_"+p.name+".tsv"))
		if err != nil {
			t.Skipf("no Skyforge route table for %s: %v", p.name, err)
		}
		if err := exec.Command("git", "-C", fwd, "rev-parse", "--verify", p.commit+"^{commit}").Run(); err != nil {
			t.Skipf("%s is not a commit in %s", p.commit, fwd)
		}
		header := strings.SplitN(string(table), "\n", 2)[0]
		if !strings.Contains(header, p.commit) && !strings.Contains(header, p.commit[:11]) {
			t.Skipf("Skyforge's %s table was generated at a different commit than the pin %s: %s", p.name, p.commit, header)
		}
		_, sources, err := archiveSources(fwd, p.commit)
		if err != nil {
			t.Fatal(err)
		}
		routes, err := extractRoutes(sources)
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, line := range strings.Split(string(table), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Split(line, "\t")
			for len(f) < 5 {
				f = append(f, "")
			}
			want = append(want, f[0]+"\t"+f[1]+"\t"+f[4]+"\t"+f[2])
		}
		sort.Strings(want)
		got := routeKeys(routes, true)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: the Go extraction differs from Skyforge's table (%d vs %d rows); first differences:\n%s", p.name, len(got), len(want), firstDifferences(got, want))
			continue
		}
		t.Logf("%s: %d routes, identical to Skyforge's table including params and file:line", p.name, len(got))
	}
}

func firstDifferences(got, want []string) string {
	in := func(list []string) map[string]bool {
		m := map[string]bool{}
		for _, s := range list {
			m[s] = true
		}
		return m
	}
	gotSet, wantSet := in(got), in(want)
	var out []string
	for _, s := range got {
		if !wantSet[s] && len(out) < 6 {
			out = append(out, "  only in Go:     "+strings.ReplaceAll(s, "\t", " | "))
		}
	}
	for _, s := range want {
		if !gotSet[s] && len(out) < 12 {
			out = append(out, "  only in Python: "+strings.ReplaceAll(s, "\t", " | "))
		}
	}
	return strings.Join(out, "\n")
}
