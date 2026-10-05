package forward

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCoverageManifestIsComplete fails when an exported service method that
// issues an HTTP request -- directly or through any chain of same-package
// helpers and sibling methods -- has no route in coverage_manifest.json.
//
// A consumer's route-liveness gate (Skyforge's forwardrawgate
// TestRouteLiveness) can only check an SDK call it can map to a route through
// this manifest. Every method missing here is a call nobody checks against the
// Forward controllers, so the manifest must cover the whole request-issuing
// surface, not just the methods somebody remembered to add.
//
// It also fails when a composite method's routes omit a route of a sibling
// service method it calls (Diffs.MaterialSummary must carry every route
// Diffs.Count, FilesCount and ChecksCount send) -- unless the caller is a
// narrowing wrapper whose routes are a strict subset of the callee's -- and
// when a manifest symbol sends no request at all: a stale entry, or a hole in
// the analysis.
func TestCoverageManifestIsComplete(t *testing.T) {
	graph := analyzeServiceRequests(t)

	// Positive controls: a direct sender, a composite that only reaches the
	// wire through sibling methods, a kind-dependent sender through a helper,
	// and one that builds its own http.Request.
	for symbol, wantVia := range map[string]string{
		"Checks.List":                                "",
		"Diffs.MaterialSummary":                      "Diffs.ChecksCount",
		"SyntheticNodes.Get":                         "",
		"SoftwareCentral.DownloadDeploymentArtifact": "SoftwareCentral.DeploymentArtifactURL",
	} {
		if !graph.issuing[symbol] {
			t.Errorf("positive control: analysis does not see %s issue a request; the call-graph walk is broken", symbol)
		}
		if wantVia != "" && !contains(graph.via[symbol], wantVia) {
			t.Errorf("positive control: analysis does not see %s call %s (via %v)", symbol, wantVia, graph.via[symbol])
		}
	}
	// Negative controls: Raw is typed-coverage-exempt by definition
	// (requireExportedSDKMethod refuses it), and Capabilities.Profile only
	// reads the client's cached profile.
	for _, symbol := range []string{"Raw.Do", "Capabilities.Profile"} {
		if graph.issuing[symbol] {
			t.Errorf("negative control: %s must not be counted as a request-issuing typed method", symbol)
		}
	}
	if len(graph.issuing) < len(sdkCoverageCatalog) {
		t.Fatalf("analysis found only %d request-issuing methods against %d catalog symbols; the check would be vacuous", len(graph.issuing), len(sdkCoverageCatalog))
	}

	var missing, stale, partial []string
	for symbol := range graph.issuing {
		if _, listed := sdkCoverageCatalog[symbol]; !listed {
			missing = append(missing, symbol)
		}
	}
	for symbol, operations := range sdkCoverageCatalog {
		if !graph.issuing[symbol] {
			stale = append(stale, symbol)
			continue
		}
		have := map[CoverageOperation]bool{}
		for _, operation := range operations {
			have[operation] = true
		}
		for _, callee := range graph.via[symbol] {
			calleeOperations := sdkCoverageCatalog[callee]
			if narrows(operations, calleeOperations) {
				// A wrapper that binds the argument selecting the route
				// (SyntheticNodes.PutL3VPN -> Put(kind=l3vpn)) sends a subset.
				continue
			}
			for _, operation := range calleeOperations {
				if !have[operation] {
					partial = append(partial, symbol+" calls "+callee+" but does not list "+operation.Method+" "+operation.Route)
				}
			}
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	sort.Strings(partial)
	if len(missing) > 0 {
		t.Errorf("%d exported SDK methods issue a request but have no route in coverage_manifest.json; add each with the method+route(s) it sends and run go generate ./...:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	if len(partial) > 0 {
		t.Errorf("%d composite routes missing from coverage_manifest.json:\n  %s", len(partial), strings.Join(partial, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("%d coverage_manifest.json symbols issue no request according to the call-graph walk (stale entry, or a path the walk cannot see):\n  %s",
			len(stale), strings.Join(stale, "\n  "))
	}
	t.Logf("%d exported service methods issue a request; %d catalog symbols", len(graph.issuing), len(sdkCoverageCatalog))
}

// narrows reports whether every operation the caller lists is one of the
// callee's: the caller pins one of several routes the callee can send.
func narrows(caller, callee []CoverageOperation) bool {
	if len(caller) == 0 || len(caller) >= len(callee) {
		return false
	}
	for _, operation := range caller {
		if !containsOperation(callee, operation) {
			return false
		}
	}
	return true
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type serviceRequestGraph struct {
	// issuing holds every Client-field service method ("Service.Method")
	// that can reach net/http.NewRequest[WithContext].
	issuing map[string]bool
	// via lists, per service method, the OTHER request-issuing service methods
	// it reaches through unexported helpers and closures (the walk stops at a
	// service method: that method's own manifest entry carries its routes).
	via map[string][]string
	// file is the source file declaring each service method.
	file map[string]string
}

// analyzeServiceRequests type-checks this package from source and walks its
// call graph. References count as well as calls, and closure bodies belong to
// the function that declares them, so a method value handed to a pager or a
// poller is still followed.
func analyzeServiceRequests(t *testing.T) serviceRequestGraph {
	t.Helper()
	fset := token.NewFileSet()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	config := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	pkg, err := config.Check("github.com/forwardnetworks/forward-go-sdk", fset, files, info)
	if err != nil {
		t.Fatalf("type-check SDK package: %v", err)
	}

	// Edges from every package-level function or method to the functions it
	// references; seeds are the functions that reference http.NewRequest*.
	edges := map[*types.Func][]*types.Func{}
	direct := map[*types.Func]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			self, _ := info.Defs[fn.Name].(*types.Func)
			if self == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				ident, ok := node.(*ast.Ident)
				if !ok {
					return true
				}
				callee, ok := info.Uses[ident].(*types.Func)
				if !ok {
					return true
				}
				callee = callee.Origin()
				if callee.Pkg() != nil && callee.Pkg().Path() == "net/http" && (callee.Name() == "NewRequest" || callee.Name() == "NewRequestWithContext") {
					direct[self] = true
				}
				if callee.Pkg() == pkg {
					edges[self] = append(edges[self], callee)
				}
				return true
			})
		}
	}
	if len(direct) == 0 {
		t.Fatal("no function in the package calls http.NewRequest*; the seed detection is broken")
	}
	// Least fixed point: a function reaches the wire when it calls
	// http.NewRequest* itself or references any function that does.
	reaches := map[*types.Func]bool{}
	for fn := range direct {
		reaches[fn] = true
	}
	for changed := true; changed; {
		changed = false
		for fn, callees := range edges {
			if reaches[fn] {
				continue
			}
			for _, callee := range callees {
				if reaches[callee] {
					reaches[fn] = true
					changed = true
					break
				}
			}
		}
	}

	clientObj, ok := pkg.Scope().Lookup("Client").(*types.TypeName)
	if !ok {
		t.Fatal("package has no Client type")
	}
	clientStruct, ok := clientObj.Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatal("Client is not a struct")
	}
	symbols := map[*types.Func]string{}
	for i := 0; i < clientStruct.NumFields(); i++ {
		field := clientStruct.Field(i)
		if !field.Exported() || field.Name() == "Raw" {
			continue
		}
		pointer, ok := field.Type().(*types.Pointer)
		if !ok {
			continue
		}
		named, ok := pointer.Elem().(*types.Named)
		if !ok || named.Obj().Pkg() != pkg {
			continue
		}
		for j := 0; j < named.NumMethods(); j++ {
			if method := named.Method(j); method.Exported() && reaches[method] {
				symbols[method] = field.Name() + "." + method.Name()
			}
		}
	}

	graph := serviceRequestGraph{issuing: map[string]bool{}, via: map[string][]string{}, file: map[string]string{}}
	for root, symbol := range symbols {
		graph.issuing[symbol] = true
		graph.file[symbol] = filepath.Base(fset.Position(root.Pos()).Filename)
		seen := map[*types.Func]bool{root: true}
		stack := append([]*types.Func(nil), edges[root]...)
		for len(stack) > 0 {
			fn := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[fn] {
				continue
			}
			seen[fn] = true
			if callee, ok := symbols[fn]; ok {
				graph.via[symbol] = append(graph.via[symbol], callee)
				continue
			}
			stack = append(stack, edges[fn]...)
		}
		sort.Strings(graph.via[symbol])
	}
	return graph
}
