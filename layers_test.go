package forward

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The SDK is built in hexagonal layers, and each may import only what its layer allows (see CLAUDE.md). This test reads the
// imports of every non-test source file and fails on any other edge, so the layout cannot erode silently in a change that
// passes every behavioural test: a domain file that reaches for net/http, an adapter that imports another adapter, a driving
// adapter that talks to a use case. It reads files, not the type checker, so it also sees an import a build tag would hide.

const layersModule = "github.com/forwardnetworks/forward-go-sdk"

type layerRule struct {
	dir     string   // the package directory, relative to the module root; "." is the root
	allowed []string // module-relative directories it may import (a prefix match, so "domain" allows domain/perfpayload)
	denyStd []string // standard-library packages it may not import
}

var layerRules = []layerRule{
	{dir: "domain", allowed: []string{"domain"}, denyStd: []string{"net/http", "net/url", "os", "os/exec", "net"}},
	{dir: "domain/perfpayload", allowed: []string{"domain"}, denyStd: []string{"net/http", "net/url", "os", "os/exec", "net"}},
	{dir: "ports", allowed: []string{"domain", "ports"}},
	{dir: "usecases", allowed: []string{"domain", "ports", "usecases"}},
	{dir: "internal/adapters/secondary/rest", allowed: []string{"ports", "internal/adapters/secondary/rest"}},
	{dir: "internal/adapters/secondary/accessfile", allowed: []string{"ports", "internal/adapters/secondary/accessfile"}},
	{dir: "cmd/fwdctl", allowed: []string{".", "ports", "cmd/fwdctl"}},
	{dir: "forwardtest", allowed: []string{".", "ports", "forwardtest"}},
}

// layerViolationsIn returns, for the non-test .go files of dir, each import the rule does not allow.
func layerViolationsIn(t *testing.T, rule layerRule) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(rule.dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		checked++
		for _, is := range f.Imports {
			path, _ := strconv.Unquote(is.Path.Value)
			switch {
			case path == layersModule || strings.HasPrefix(path, layersModule+"/"):
				rel := strings.TrimPrefix(strings.TrimPrefix(path, layersModule), "/")
				if rel == "" {
					rel = "."
				}
				if !importAllowed(rel, rule.allowed) {
					out = append(out, file+" imports "+path)
				}
			default:
				for _, denied := range rule.denyStd {
					if path == denied {
						out = append(out, file+" imports "+path+", which this layer may not")
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatalf("%s: no source files read; the check proves nothing", rule.dir)
	}
	sort.Strings(out)
	return out
}

func importAllowed(rel string, allowed []string) bool {
	for _, a := range allowed {
		if rel == a || (a != "." && strings.HasPrefix(rel, a+"/")) {
			return true
		}
	}
	return false
}

func TestEveryLayerImportsOnlyWhatItMay(t *testing.T) {
	for _, rule := range layerRules {
		t.Run(rule.dir, func(t *testing.T) {
			if v := layerViolationsIn(t, rule); len(v) != 0 {
				t.Errorf("layer %s breaks its import rule:\n  %s", rule.dir, strings.Join(v, "\n  "))
			}
		})
	}
}

// Only the composition root may import an adapter, and only composition-root.go does: any other root source file naming one
// would put a second wiring point in the module.
func TestOnlyTheCompositionRootImportsAnAdapter(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var importers []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, is := range f.Imports {
			if path, _ := strconv.Unquote(is.Path.Value); strings.HasPrefix(path, layersModule+"/internal/adapters/") {
				importers = append(importers, file)
				break
			}
		}
	}
	if len(importers) != 1 || importers[0] != "composition-root.go" {
		t.Errorf("root files importing an adapter = %v, want exactly [composition-root.go]", importers)
	}
	if _, err := os.Stat("composition-root.go"); err != nil {
		t.Fatalf("no composition-root.go: %v", err)
	}
}

// The checker must be able to fail: each rule is applied to a scratch package that breaks it.
func TestTheLayerCheckCatchesABrokenRule(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package x\n\nimport (\n\t\"net/http\"\n\t_ \""+layersModule+"/usecases\"\n)\n\nvar _ = http.StatusOK\n")
	write("b_test.go", "package x\n\nimport _ \""+layersModule+"/internal/adapters/secondary/rest\"\n")
	v := layerViolationsIn(t, layerRule{dir: dir, allowed: []string{"domain"}, denyStd: []string{"net/http"}})
	got := strings.Join(v, "\n")
	if !strings.Contains(got, "imports net/http") || !strings.Contains(got, layersModule+"/usecases") {
		t.Errorf("violations = %q, want the net/http and the usecases imports", v)
	}
	if strings.Contains(got, "b_test.go") {
		t.Errorf("a test file was checked: %q", v)
	}
}
