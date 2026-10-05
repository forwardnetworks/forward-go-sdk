package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Route is one HTTP mapping found in Forward's web sources.
type Route struct {
	Method     string // GET, POST, ... or ANY for a @RequestMapping without a method
	Path       string // starts with /api; "{?Expr}" marks a constant the extractor could not resolve
	Source     string // file:line relative to web/src/main
	Deprecated bool
	Params     string // the annotation's params = ... text, informational
	Profile    string // the controller class's @ProfileCriteria / @Profile text, informational
}

// extractRoutes is a Go port of Skyforge's routes.py and security_routes.py (internal/forwardrawgate/routes): the
// routes Forward's @*Mapping controller annotations declare, plus the ones Spring Security filters serve. sources maps
// a path relative to web/src/main (for example "java/com/forwardnetworks/cv/web/controller/AliasController.java") to its
// text. A Java parser would be exact; a regex pass over annotations is what the Python tool did and what the tables
// Skyforge pins were generated with, and TestExtractMatchesSkyforgeTables holds the port to that output.
func extractRoutes(sources map[string]string) ([]Route, error) {
	var routes []Route
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		routes = append(routes, controllerRoutes(name, sources[name])...)
	}
	security, err := securityRoutes(sources)
	if err != nil {
		return nil, err
	}
	return append(routes, security...), nil
}

var (
	annRe       = regexp.MustCompile(`@(GetMapping|PostMapping|PutMapping|PatchMapping|DeleteMapping|RequestMapping)\b`)
	controllerR = regexp.MustCompile(`@(Rest)?Controller\b`)
	classRe     = regexp.MustCompile(`\n(?:public\s+|final\s+|abstract\s+)*(?:class|interface)\s+\w+`)
	constRe     = regexp.MustCompile(`(?:static\s+final|final\s+static)\s+String\s+(\w+)\s*=\s*([^;]+);`)
	exprPartRe  = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"|([A-Za-z_][\w\.]*)`)
	valuePathRe = regexp.MustCompile(`\b(?:value|path)\s*=\s*(\{[^}]*\}|[^,)]+(?:\+[^,)]+)*)`)
	namedArgRe  = regexp.MustCompile(`^\s*\w+\s*=`)
	nextArgRe   = regexp.MustCompile(`,\s*\w+\s*=`)
	methodRe    = regexp.MustCompile(`RequestMethod\.(\w+)`)
	paramsRe    = regexp.MustCompile(`\bparams\s*=\s*(\{[^}]*\}|"[^"]*")`)
	bodyEndRe   = regexp.MustCompile(`\)\s*(?:throws[^{;]*)?\{`)
	slashesRe   = regexp.MustCompile(`//+`)
	profileRe   = regexp.MustCompile(`@(ProfileCriteria|Profile)\b`)
)

var mappingMethod = map[string]string{
	"GetMapping": "GET", "PostMapping": "POST", "PutMapping": "PUT", "PatchMapping": "PATCH", "DeleteMapping": "DELETE",
}

// readAnnArgs returns the text between the parentheses of the annotation whose name ends just before i, and the index
// after the closing parenthesis. No parentheses gives ("", i).
func readAnnArgs(src string, i int) (string, int) {
	j := i
	for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n') {
		j++
	}
	if j >= len(src) || src[j] != '(' {
		return "", i
	}
	depth, inString := 0, false
	for k := j; k < len(src); k++ {
		ch := src[k]
		if inString {
			if ch == '\\' {
				k++
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[j+1 : k], k + 1
			}
		}
	}
	return src[j+1:], len(src)
}

func resolveConsts(src string) map[string]string {
	consts := map[string]string{}
	for _, m := range constRe.FindAllStringSubmatch(src, -1) {
		consts[m[1]] = m[2]
	}
	return consts
}

// evalExpr evaluates a Java string concatenation of literals and constants. A constant it cannot resolve becomes
// "{?Name}" so the route is still listed and visibly incomplete.
func evalExpr(expr string, consts map[string]string, depth int) string {
	var out strings.Builder
	for _, m := range exprPartRe.FindAllStringSubmatch(expr, -1) {
		lit, ident := m[1], m[2]
		if lit != "" || ident == "" {
			out.WriteString(lit)
			continue
		}
		name := ident[strings.LastIndex(ident, ".")+1:]
		if value, ok := consts[name]; ok && depth < 5 {
			out.WriteString(evalExpr(value, consts, depth+1))
		} else if ident == "ServletPaths.API" {
			out.WriteString("/api")
		} else {
			out.WriteString("{?" + ident + "}")
		}
	}
	return out.String()
}

func pathsOf(args string, consts map[string]string) []string {
	if strings.TrimSpace(args) == "" {
		return []string{""}
	}
	var v string
	if m := valuePathRe.FindStringSubmatch(args); m != nil {
		v = m[1]
	} else {
		if namedArgRe.MatchString(args) {
			return []string{""}
		}
		v = args
		if loc := nextArgRe.FindStringIndex(v); loc != nil {
			v = v[:loc[0]]
		}
	}
	v = strings.TrimSpace(v)
	var items []string
	if strings.HasPrefix(v, "{") {
		for _, part := range strings.Split(strings.Trim(v, "{}"), ",") {
			if strings.TrimSpace(part) != "" {
				items = append(items, strings.TrimSpace(part))
			}
		}
	} else {
		items = []string{v}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, evalExpr(item, consts, 0))
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func methodsOf(args string) []string {
	var methods []string
	for _, m := range methodRe.FindAllStringSubmatch(args, -1) {
		methods = append(methods, m[1])
	}
	if len(methods) == 0 {
		return []string{"ANY"}
	}
	return methods
}

func paramsOf(args string) string {
	if m := paramsRe.FindStringSubmatch(args); m != nil {
		return strings.ReplaceAll(m[1], "\n", " ")
	}
	return ""
}

func lastIndexByte(s string, end int, chars string) int {
	best := -1
	for _, c := range chars {
		if i := strings.LastIndex(s[:end], string(c)); i > best {
			best = i
		}
	}
	return best
}

func controllerRoutes(file, src string) []Route {
	if !controllerR.MatchString(src) {
		return nil
	}
	consts := resolveConsts(src)
	loc := classRe.FindStringIndex(src)
	if loc == nil {
		return nil
	}
	head, body := src[:loc[0]], src[loc[0]:]
	prefixes := []string{""}
	for _, m := range annRe.FindAllStringSubmatchIndex(head, -1) {
		if head[m[2]:m[3]] != "RequestMapping" {
			continue
		}
		args, _ := readAnnArgs(head, m[1])
		if paths := pathsOf(args, consts); anyNonEmpty(paths) {
			prefixes = paths
		}
	}
	profile := classProfile(head)
	baseLine := strings.Count(head, "\n") + 1
	var routes []Route
	for _, m := range annRe.FindAllStringSubmatchIndex(body, -1) {
		kind := body[m[2]:m[3]]
		args, end := readAnnArgs(body, m[1])
		start := lastIndexByte(body, m[0], ";}{")
		if start < 0 {
			start = 0
		}
		stop := end
		if mb := bodyEndRe.FindStringIndex(body[end:]); mb != nil {
			stop = end + mb[0]
		}
		deprecated := strings.Contains(body[start:stop], "@Deprecated")
		line := baseLine + strings.Count(body[:m[0]], "\n")
		methods := []string{mappingMethod[kind]}
		if kind == "RequestMapping" {
			methods = methodsOf(args)
		}
		for _, prefix := range prefixes {
			for _, p := range pathsOf(args, consts) {
				full := slashesRe.ReplaceAllString("/api"+prefix+p, "/")
				for _, method := range methods {
					routes = append(routes, Route{Method: method, Path: full, Source: fmt.Sprintf("%s:%d", file, line),
						Deprecated: deprecated, Params: paramsOf(args), Profile: profile})
				}
			}
		}
	}
	return routes
}

func anyNonEmpty(values []string) bool {
	for _, v := range values {
		if v != "" {
			return true
		}
	}
	return false
}

// classProfile returns the controller's @ProfileCriteria / @Profile arguments, whitespace collapsed, or "".
func classProfile(head string) string {
	var parts []string
	for _, m := range profileRe.FindAllStringSubmatchIndex(head, -1) {
		args, _ := readAnnArgs(head, m[1])
		name := head[m[2]:m[3]]
		parts = append(parts, name+"("+strings.Join(strings.Fields(args), " ")+")")
	}
	return strings.Join(parts, " ")
}

var (
	loginPageRe      = regexp.MustCompile(`\.loginPage\(\s*([\w\.]+)\s*\)`)
	loginProcessRe   = regexp.MustCompile(`\.loginProcessingUrl\(\s*(PagePaths\.[\w]+|"[^"]*")\s*\)`)
	switchUserRe     = regexp.MustCompile(`setSwitchUserMatcher\(\s*MATCHERS\.matcher\(\s*HttpMethod\.(\w+)\s*,\s*([\w\.]+)\s*\)\s*\)`)
	exitUserRe       = regexp.MustCompile(`setExitUserMatcher\(\s*MATCHERS\.matcher\(\s*HttpMethod\.(\w+)\s*,\s*([\w\.]+)\s*\)\s*\)`)
	staticStringRe   = regexp.MustCompile(`static\s+final\s+String\s+(\w+)\s*=\s*"([^"]*)"`)
	samlRequestRe    = regexp.MustCompile(`matcher\(\s*Saml2AuthenticationRequestResolver\.DEFAULT_AUTHENTICATION_REQUEST_URI`)
	samlProcessingRe = regexp.MustCompile(`\.loginProcessingUrl\(\s*Saml2WebSsoAuthenticationFilter\.DEFAULT_FILTER_PROCESSES_URI`)
)

const (
	securityConfigFile = "java/com/forwardnetworks/cv/web/config/SecurityConfig.java"
	pagePathsFile      = "java/com/forwardnetworks/cv/web/PagePaths.java"
	samlConfigFile     = "java/com/forwardnetworks/cv/web/config/SamlSecurityConfig.java"
)

// securityRoutes finds the routes Spring Security filters serve, which no controller declares: the form login page and
// processing URL, user switching, and the SAML sign-in chain. It fails closed, as security_routes.py did, when the
// config no longer has the shape it expects, so a refactor cannot silently drop them from the table.
func securityRoutes(sources map[string]string) ([]Route, error) {
	security, ok := sources[securityConfigFile]
	if !ok {
		return nil, fmt.Errorf("routecheck: %s is not in the sources", securityConfigFile)
	}
	consts := map[string]string{}
	for _, name := range []string{securityConfigFile, pagePathsFile} {
		for _, m := range staticStringRe.FindAllStringSubmatch(sources[name], -1) {
			consts[m[1]] = m[2]
		}
	}
	resolve := func(expr string) (string, error) {
		expr = strings.TrimSpace(expr)
		if strings.HasPrefix(expr, `"`) {
			return strings.Trim(expr, `"`), nil
		}
		name := expr[strings.LastIndex(expr, ".")+1:]
		value, ok := consts[name]
		if !ok {
			return "", fmt.Errorf("routecheck: cannot resolve %q in SecurityConfig.java", expr)
		}
		return value, nil
	}
	line := func(src string, offset int) int { return strings.Count(src[:offset], "\n") + 1 }
	var routes []Route
	found := 0
	for _, rule := range []struct {
		re     *regexp.Regexp
		method string
		what   string
	}{{loginPageRe, "GET", "formLogin.loginPage"}, {loginProcessRe, "POST", "formLogin.loginProcessingUrl"}} {
		for _, m := range rule.re.FindAllStringSubmatchIndex(security, -1) {
			route, err := resolve(security[m[2]:m[3]])
			if err != nil {
				return nil, err
			}
			routes = append(routes, Route{Method: rule.method, Path: route, Source: fmt.Sprintf("%s:%d (%s)", securityConfigFile, line(security, m[0]), rule.what)})
			found++
		}
	}
	for _, rule := range []struct {
		re   *regexp.Regexp
		what string
	}{{switchUserRe, "SwitchUserFilter switch"}, {exitUserRe, "SwitchUserFilter exit"}} {
		for _, m := range rule.re.FindAllStringSubmatchIndex(security, -1) {
			route, err := resolve(security[m[4]:m[5]])
			if err != nil {
				return nil, err
			}
			routes = append(routes, Route{Method: security[m[2]:m[3]], Path: route, Source: fmt.Sprintf("%s:%d (%s)", securityConfigFile, line(security, m[0]), rule.what)})
			found++
		}
	}
	if found < 4 {
		return nil, fmt.Errorf("routecheck: expected the form login, login page and switch-user matchers in SecurityConfig.java, found %d; it changed shape", found)
	}
	saml, ok := sources[samlConfigFile]
	if !ok {
		return nil, fmt.Errorf("routecheck: %s is not in the sources", samlConfigFile)
	}
	for _, rule := range []struct {
		re            *regexp.Regexp
		method, route string
		what          string
	}{
		{samlRequestRe, "GET", "/saml2/authenticate/{registrationId}", "saml2Login authenticationRequest"},
		{samlProcessingRe, "POST", "/login/saml2/sso/{registrationId}", "saml2Login loginProcessingUrl"},
	} {
		loc := rule.re.FindStringIndex(saml)
		if loc == nil {
			return nil, fmt.Errorf("routecheck: %s not found in SamlSecurityConfig.java; the SAML chain changed shape", rule.what)
		}
		routes = append(routes, Route{Method: rule.method, Path: rule.route, Source: fmt.Sprintf("%s:%d (%s)", samlConfigFile, line(saml, loc[0]), rule.what)})
	}
	return routes, nil
}

// archiveSources reads every .java file under web/src/main/java at a commit of a Forward git repository, keyed by path
// relative to web/src/main. It fails when the commit does not exist or yields no sources.
func archiveSources(gitDir, commit string) (resolved string, sources map[string]string, err error) {
	out, err := exec.Command("git", "-C", gitDir, "rev-parse", "--verify", commit+"^{commit}").Output()
	if err != nil {
		return "", nil, fmt.Errorf("routecheck: %s is not a commit in %s: %w", commit, gitDir, err)
	}
	resolved = strings.TrimSpace(string(out))
	archive, err := exec.Command("git", "-C", gitDir, "archive", resolved, "web/src/main/java").Output()
	if err != nil {
		return "", nil, fmt.Errorf("routecheck: git archive %s: %w", resolved, err)
	}
	sources = map[string]string{}
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("routecheck: reading the archive of %s: %w", resolved, err)
		}
		if header.Typeflag != tar.TypeReg || path.Ext(header.Name) != ".java" {
			continue
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return "", nil, err
		}
		sources[strings.TrimPrefix(header.Name, "web/src/main/")] = string(data)
	}
	if len(sources) == 0 {
		return "", nil, fmt.Errorf("routecheck: no web/src/main/java sources at %s", resolved)
	}
	return resolved, sources, nil
}
