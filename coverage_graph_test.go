package forward

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// updateServiceGraph records the module analyzer's lines for symbols added since the hexagonal split (the v0.9 hexagonal split) in
// testdata/service_request_graph_added.txt. testdata/service_request_graph.txt was recorded on the unmoved tree, where both
// analyzers agreed, and is pinned by hash: it is never rewritten, and an update refuses if a recorded symbol changed.
var updateServiceGraph = flag.Bool("update-service-graph", false, "record symbols added since the split in testdata/service_request_graph_added.txt")

const serviceGraphPath = "testdata/service_request_graph.txt"

// serviceGraphSHA256 pins the recorded graph, so a rewrite of it is a reviewed change to this line, not a silent one.
const serviceGraphSHA256 = "f90ec0986dc1792de196f1e9c9a1d733e274d93faba49aa18ce857a513e8ad3c"

// adapterPackageSuffix names the package whose methods implement the ports: the one the composition root wires. An interface call
// resolves only to implementations there, so an unwired second implementation (a legacy adapter, a fake, a wrapper) can never make
// a symbol look like it sends.
const adapterPackageSuffix = "/internal/adapters/secondary/rest"

// reporter is the part of testing.TB the analyzer uses, so a test can check that a bad shape is reported.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// analyzeModuleRequests is analyzeServiceRequests for a module of several packages: it type-checks the root package and every
// package of this module the root depends on, from source, and walks one call graph across all of them. What differs from a
// single-package walk, each needed by the hexagonal split (the v0.9 hexagonal split):
//   - an edge to a function in any module package is followed, not only the root's own;
//   - a call through a module interface method (a port) is an edge to its implementation in the wired adapter package
//     (adapterPackageSuffix), and there must be exactly one: none, or two, is an error, never a silent guess;
//   - a symbol is any exported method in the full method set of a Client field's pointer type, promoted ones included, so a method a
//     *usecases.X gets from its embedded port is still Service.Method; a promoted port method with no implementation is an error.
//
// It refuses every shape it cannot read rather than skipping it: an exported Client field that is not a pointer to a module struct
// type (an alias is read through), a module package with cgo or build-constraint-excluded files, a reference to http.NewRequest* that
// is not a direct call, an http.Request literal, and the net/http convenience senders (http.Get and friends, and the same methods on
// an *http.Client), none of which the walk would see as sending.
func analyzeModuleRequests(t *testing.T) serviceRequestGraph {
	t.Helper()
	return analyzeModuleRequestsIn(t, ".")
}

// analyzeModuleRequestsIn runs the analysis on the module whose root package is in dir.
func analyzeModuleRequestsIn(r reporter, dir string) serviceRequestGraph {
	r.Helper()
	pkgs := loadModulePackages(r, dir)
	root := pkgs.checked[pkgs.module]
	if root == nil {
		r.Fatalf("the root package %s was not type-checked", pkgs.module)
	}
	isModule := func(pkg *types.Package) bool {
		return pkg != nil && pkgs.checked[pkg.Path()] == pkg
	}

	// Every concrete type declared in the wired adapter package, for resolving interface calls. A type's methods are looked up in its
	// full method set, so a port method a rest type gets by embedding a shared base is found.
	var adapterTypes []*types.Named
	for _, path := range pkgs.order {
		if !strings.HasSuffix(path, adapterPackageSuffix) {
			continue
		}
		scope := pkgs.checked[path].Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok || tn.IsAlias() {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			if _, isInterface := named.Underlying().(*types.Interface); isInterface {
				continue
			}
			adapterTypes = append(adapterTypes, named)
		}
	}
	implementations := map[*types.Func][]*types.Func{}
	implementationsOf := func(abstract *types.Func) []*types.Func {
		if impls, ok := implementations[abstract]; ok {
			return impls
		}
		signature := abstract.Type().(*types.Signature)
		iface, _ := signature.Recv().Type().Underlying().(*types.Interface)
		var out []*types.Func
		if iface != nil {
			for _, named := range adapterTypes {
				pointer := types.NewPointer(named)
				if !types.Implements(named, iface) && !types.Implements(pointer, iface) {
					continue
				}
				selection := types.NewMethodSet(pointer).Lookup(abstract.Pkg(), abstract.Name())
				if selection == nil {
					continue
				}
				method, ok := selection.Obj().(*types.Func)
				if ok && isInterfaceMethod(method) {
					// A rest type that embeds a port (a decorator) answers with the port's own method, which has no body to walk.
					r.Errorf("%s answers %s through an embedded interface; the walk cannot see what it calls", named.Obj().Name(), abstract.FullName())
					continue
				}
				if ok && !containsFunc(out, method) {
					out = append(out, method) // a wrapper that embeds an implementation shares its methods: one, not two
				}
			}
		}
		if len(out) > 1 {
			var names []string
			for _, method := range out {
				names = append(names, method.FullName())
			}
			r.Errorf("%s has %d implementations in the adapter package (%s); the walk cannot tell which one is wired", abstract.FullName(), len(out), strings.Join(names, ", "))
		}
		implementations[abstract] = out
		return out
	}

	edges := map[*types.Func][]*types.Func{}
	direct := map[*types.Func]bool{}
	for _, path := range pkgs.order {
		info := pkgs.info[path]
		for _, file := range pkgs.files[path] {
			// A reference to a request constructor or sender anywhere in the file is checked; a direct call inside a function is a
			// seed, anything else is a path the walk could not follow.
			calledDirectly := map[*ast.Ident]bool{}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					calledDirectly[fun] = true
				case *ast.SelectorExpr:
					calledDirectly[fun.Sel] = true
				}
				return true
			})
			ast.Inspect(file, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.CompositeLit:
					if named, ok := derefType(info.TypeOf(n)).(*types.Named); ok && isHTTPRequest(named) {
						r.Errorf("%s: an http.Request literal sends a request the walk cannot see; build it with http.NewRequestWithContext", pkgs.fset.Position(n.Pos()))
					}
				case *ast.CallExpr:
					if builtin, ok := n.Fun.(*ast.Ident); ok && builtin.Name == "new" && len(n.Args) == 1 && info.Uses[builtin] == types.Universe.Lookup("new") {
						if named, ok := derefType(info.TypeOf(n.Args[0])).(*types.Named); ok && isHTTPRequest(named) {
							r.Errorf("%s: new(http.Request) builds a request the walk cannot see; build it with http.NewRequestWithContext", pkgs.fset.Position(n.Pos()))
						}
					}
				case *ast.ValueSpec:
					if n.Type != nil {
						if named, ok := types.Unalias(info.TypeOf(n.Type)).(*types.Named); ok && isHTTPRequest(named) {
							r.Errorf("%s: a variable of type http.Request builds a request the walk cannot see; build it with http.NewRequestWithContext", pkgs.fset.Position(n.Pos()))
						}
					}
				case *ast.Ident:
					fn, ok := info.Uses[n].(*types.Func)
					if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "net/http" {
						return true
					}
					switch {
					case isRequestConstructor(fn) && !calledDirectly[n]:
						r.Errorf("%s: %s is referenced, not called; the walk only sees a direct call", pkgs.fset.Position(n.Pos()), fn.FullName())
					case isConvenienceSender(fn):
						r.Errorf("%s: %s sends a request the walk cannot see; build it with http.NewRequestWithContext and Do", pkgs.fset.Position(n.Pos()), fn.FullName())
					}
				}
				return true
			})
			for _, decl := range file.Decls {
				// A function literal outside any function (a package-level var) is never walked, so a request it builds is not a seed.
				if gen, ok := decl.(*ast.GenDecl); ok {
					ast.Inspect(gen, func(node ast.Node) bool {
						ident, ok := node.(*ast.Ident)
						if !ok {
							return true
						}
						if fn, ok := info.Uses[ident].(*types.Func); ok && fn.Pkg() != nil && fn.Pkg().Path() == "net/http" && isRequestConstructor(fn) {
							r.Errorf("%s: %s is used outside a function declaration; the walk only follows function bodies", pkgs.fset.Position(ident.Pos()), fn.FullName())
						}
						return true
					})
					continue
				}
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
					if callee.Pkg() != nil && callee.Pkg().Path() == "net/http" && isRequestConstructor(callee) {
						direct[self] = true
					}
					if !isModule(callee.Pkg()) {
						return true
					}
					if isInterfaceMethod(callee) {
						impls := implementationsOf(callee)
						if len(impls) == 0 && isPortInterface(callee, path) {
							r.Errorf("%s: %s calls %s, which has no implementation in the adapter package; the walk cannot follow the call", pkgs.fset.Position(ident.Pos()), self.FullName(), callee.FullName())
						}
						edges[self] = append(edges[self], impls...)
						return true
					}
					edges[self] = append(edges[self], callee)
					return true
				})
			}
		}
	}
	if len(direct) == 0 {
		r.Fatalf("no function in the module calls http.NewRequest*; the seed detection is broken")
	}
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

	clientObj, ok := root.Scope().Lookup("Client").(*types.TypeName)
	if !ok {
		r.Fatalf("root package has no Client type")
	}
	clientStruct, ok := clientObj.Type().Underlying().(*types.Struct)
	if !ok {
		r.Fatalf("Client is not a struct")
	}
	type rootedSymbol struct {
		name   string
		starts []*types.Func
	}
	var symbols []rootedSymbol
	symbolOf := map[*types.Func]string{}
	for i := 0; i < clientStruct.NumFields(); i++ {
		field := clientStruct.Field(i)
		if !field.Exported() || field.Name() == "Raw" {
			continue
		}
		pointer, ok := types.Unalias(field.Type()).(*types.Pointer)
		if !ok {
			r.Errorf("Client.%s is %s, not a pointer to a service type; the walk cannot read it", field.Name(), field.Type())
			continue
		}
		named, ok := types.Unalias(pointer.Elem()).(*types.Named)
		if !ok || !isModule(named.Obj().Pkg()) {
			r.Errorf("Client.%s is %s, not a pointer to a type of this module; the walk cannot read it", field.Name(), field.Type())
			continue
		}
		if _, isInterface := named.Underlying().(*types.Interface); isInterface {
			r.Errorf("Client.%s points to an interface; a service field is a pointer to a struct", field.Name())
			continue
		}
		methods := types.NewMethodSet(types.NewPointer(named))
		for j := 0; j < methods.Len(); j++ {
			method, ok := methods.At(j).Obj().(*types.Func)
			if !ok || !method.Exported() {
				continue
			}
			name := field.Name() + "." + method.Name()
			starts := []*types.Func{method}
			if isInterfaceMethod(method) {
				starts = implementationsOf(method)
				if len(starts) == 0 {
					r.Errorf("%s is promoted from %s, which has no implementation in the adapter package; the walk cannot tell whether it sends", name, method.FullName())
					continue
				}
			}
			reached := false
			for _, start := range starts {
				if reaches[start] {
					reached = true
				}
			}
			if !reached {
				continue
			}
			symbols = append(symbols, rootedSymbol{name: name, starts: starts})
			for _, start := range starts {
				if other, taken := symbolOf[start]; taken && other != name {
					r.Errorf("%s and %s share the implementation %s; a via through it cannot be credited to one of them", other, name, start.FullName())
				}
				symbolOf[start] = name
			}
			// A use case that declares a method its port also has (a nil-safe wrapper, or a forwarder where the port is held in
			// a named field) stands in front of the port's implementation: that implementation is the same symbol, so a call to
			// it from elsewhere in the adapter is credited to this symbol too.
			if !isInterfaceMethod(method) {
				for _, impl := range portImplementationsBehind(named, method.Name(), implementationsOf) {
					if other, taken := symbolOf[impl]; taken && other != name {
						r.Errorf("%s and %s share the implementation %s; a via through it cannot be credited to one of them", other, name, impl.FullName())
					}
					symbolOf[impl] = name
				}
			}
		}
	}

	graph := serviceRequestGraph{issuing: map[string]bool{}, via: map[string][]string{}}
	for _, symbol := range symbols {
		graph.issuing[symbol.name] = true
		seen := map[*types.Func]bool{}
		var stack []*types.Func
		for _, start := range symbol.starts {
			seen[start] = true
			stack = append(stack, edges[start]...)
		}
		var via []string
		for len(stack) > 0 {
			fn := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[fn] {
				continue
			}
			seen[fn] = true
			if callee, ok := symbolOf[fn]; ok && callee != symbol.name {
				if !contains(via, callee) {
					via = append(via, callee)
				}
				continue
			}
			// The symbol's own implementation behind its use case is walked through, not stopped at.
			stack = append(stack, edges[fn]...)
		}
		sort.Strings(via)
		if len(via) != 0 {
			graph.via[symbol.name] = via
		}
	}
	return graph
}

// portImplementationsBehind returns the adapter implementations of the port method name on every port the use-case struct
// holds in a field (embedded or named), when that port declares such a method.
func portImplementationsBehind(useCase *types.Named, name string, implementationsOf func(*types.Func) []*types.Func) []*types.Func {
	st, ok := useCase.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	var out []*types.Func
	for i := 0; i < st.NumFields(); i++ {
		iface, ok := types.Unalias(st.Field(i).Type()).Underlying().(*types.Interface)
		if !ok {
			continue
		}
		for j := 0; j < iface.NumMethods(); j++ {
			if m := iface.Method(j); m.Name() == name {
				out = append(out, implementationsOf(m)...)
			}
		}
	}
	return out
}

func containsFunc(fns []*types.Func, want *types.Func) bool {
	for _, fn := range fns {
		if fn == want {
			return true
		}
	}
	return false
}

// isPortInterface reports whether a call from callerPkg through fn must reach the wired adapter: fn belongs to an interface declared
// in a module's ports or usecases package, or to an anonymous interface (a field typed interface{ ... }) called from a usecases
// package, whose implementation the walk could otherwise never name. An interface the module declares for its own values
// (DiscoveryParams), and an anonymous one used to inspect a value outside usecases (retry.go's interface{ Permanent() bool }), are
// not ports.
func isPortInterface(fn *types.Func, callerPkg string) bool {
	recv := fn.Type().(*types.Signature).Recv().Type()
	named, ok := types.Unalias(recv).(*types.Named)
	if !ok {
		return strings.HasSuffix(callerPkg, "/usecases")
	}
	if named.Obj().Pkg() == nil {
		return false
	}
	path := named.Obj().Pkg().Path()
	return strings.HasSuffix(path, "/ports") || strings.HasSuffix(path, "/usecases")
}

func isRequestConstructor(fn *types.Func) bool {
	return fn.Name() == "NewRequest" || fn.Name() == "NewRequestWithContext"
}

// isConvenienceSender reports net/http's functions and *http.Client methods that build and send a request in one call.
func isConvenienceSender(fn *types.Func) bool {
	switch fn.Name() {
	case "Get", "Post", "Head", "PostForm":
	default:
		return false
	}
	signature := fn.Type().(*types.Signature)
	if signature.Recv() == nil {
		return true // http.Get and friends
	}
	named, ok := derefType(signature.Recv().Type()).(*types.Named)
	return ok && named.Obj().Name() == "Client"
}

// isHTTPRequest reports net/http.Request itself. A defined type over it (type myRequest http.Request) is a different type and is not
// recognised; nothing in the module declares one.
func isHTTPRequest(named *types.Named) bool {
	return named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "net/http" && named.Obj().Name() == "Request"
}

func isInterfaceMethod(fn *types.Func) bool {
	signature, ok := fn.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return false
	}
	_, isInterface := signature.Recv().Type().Underlying().(*types.Interface)
	return isInterface
}

func derefType(t types.Type) types.Type {
	t = types.Unalias(t)
	if pointer, ok := t.(*types.Pointer); ok {
		return types.Unalias(pointer.Elem())
	}
	return t
}

type modulePackages struct {
	module  string // the main module's path
	fset    *token.FileSet
	order   []string // dependencies first
	checked map[string]*types.Package
	files   map[string][]*ast.File
	info    map[string]*types.Info
}

// loadModulePackages lists the root package and every package of the main module it depends on (go list -deps, which prints
// dependencies before their importers), then type-checks them from source in that order; the standard library comes from the
// source importer. A module package with cgo files or files a build constraint excludes is refused: its methods would be invisible.
func loadModulePackages(r reporter, dir string) modulePackages {
	r.Helper()
	cmd := exec.Command("go", "list", "-deps", "-json", ".")
	cmd.Dir = dir
	// A go.work above the module or GOFLAGS such as -mod=vendor would change what is listed; the walk reads the module alone.
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		r.Fatalf("go list -deps in %s: %v: %s", dir, err, stderr.String())
	}
	type listed struct {
		ImportPath     string
		Dir            string
		GoFiles        []string
		CgoFiles       []string
		IgnoredGoFiles []string
		Module         *struct {
			Path string
			Main bool
		}
	}
	out := modulePackages{fset: token.NewFileSet(), checked: map[string]*types.Package{}, files: map[string][]*ast.File{}, info: map[string]*types.Info{}}
	imp := moduleImporter{pkgs: &out, fallback: importer.ForCompiler(out.fset, "source", nil)}
	decoder := json.NewDecoder(&stdout)
	for decoder.More() {
		var pkg listed
		if err := decoder.Decode(&pkg); err != nil {
			r.Fatalf("decode go list: %v", err)
		}
		if pkg.Module == nil || !pkg.Module.Main {
			continue
		}
		out.module = pkg.Module.Path
		if len(pkg.CgoFiles) != 0 || len(pkg.IgnoredGoFiles) != 0 {
			r.Errorf("%s has cgo or build-constraint-excluded files (%v %v); the walk would not see them", pkg.ImportPath, pkg.CgoFiles, pkg.IgnoredGoFiles)
		}
		var files []*ast.File
		for _, name := range pkg.GoFiles {
			path := filepath.Join(pkg.Dir, name)
			src, err := os.ReadFile(path)
			if err != nil {
				r.Fatalf("%v", err)
			}
			file, err := parser.ParseFile(out.fset, path, src, parser.SkipObjectResolution)
			if err != nil {
				r.Fatalf("%v", err)
			}
			files = append(files, file)
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
		checked, err := (&types.Config{Importer: imp}).Check(pkg.ImportPath, out.fset, files, info)
		if err != nil {
			r.Fatalf("type-check %s: %v", pkg.ImportPath, err)
		}
		out.order = append(out.order, pkg.ImportPath)
		out.checked[pkg.ImportPath] = checked
		out.files[pkg.ImportPath] = files
		out.info[pkg.ImportPath] = info
	}
	if len(out.order) == 0 {
		r.Fatalf("no package of the main module was loaded from %s", dir)
	}
	return out
}

type moduleImporter struct {
	pkgs     *modulePackages
	fallback types.Importer
}

func (m moduleImporter) Import(path string) (*types.Package, error) {
	if pkg, ok := m.pkgs.checked[path]; ok {
		return pkg, nil
	}
	if module := m.pkgs.module; module != "" && (path == module || strings.HasPrefix(path, module+"/")) {
		return nil, fmt.Errorf("module package %s imported before it was checked", path)
	}
	return m.fallback.Import(path)
}

// renderServiceGraph is the graph as text: one line per request-issuing symbol, with the other symbols it reaches. The file a
// method is declared in is left out, because the split moves every one (nothing reads it).
func renderServiceGraph(graph serviceRequestGraph) string {
	symbols := make([]string, 0, len(graph.issuing))
	for symbol := range graph.issuing {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	var b strings.Builder
	for _, symbol := range symbols {
		b.WriteString(symbol)
		if via := graph.via[symbol]; len(via) != 0 {
			b.WriteString(" -> ")
			b.WriteString(strings.Join(via, " "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// The hexagonal split (the v0.9 hexagonal split) replaces the single-package walk with analyzeModuleRequests. That swap edits the gate, so it
// is held three ways: the recorded graph is pinned by its SHA-256; the module analyzer must reproduce it; and while the single-package
// analyzer can still see the code (until the move), it must agree too. An update writes the reference analyzer's graph, and only
// when both agree.
func TestServiceRequestAnalyzersAgree(t *testing.T) {
	got := renderServiceGraph(analyzeModuleRequests(t))
	want, err := os.ReadFile(serviceGraphPath)
	if err != nil {
		t.Fatalf("%v (the graph is recorded once, on the unmoved tree)", err)
	}
	if sum := sha256.Sum256(want); hex.EncodeToString(sum[:]) != serviceGraphSHA256 {
		t.Fatalf("%s has SHA-256 %x, not the pinned %s: the recorded graph was changed", serviceGraphPath, sum, serviceGraphSHA256)
	}
	if *updateServiceGraph {
		// Only symbols added since the split are recorded, in the companion file; a recorded symbol that changed is refused.
		writeAddedLines(t, serviceGraphPath, got, sameText)
		return
	}
	if reference := string(readLineReference(t, serviceGraphPath)); reference != got {
		t.Errorf("the module analyzer's graph differs from %s and %s:\n%s", serviceGraphPath, addedPath(serviceGraphPath), lineDiff(reference, got))
	}
}

func lineDiff(want, got string) string {
	wantLines := map[string]bool{}
	for _, line := range strings.Split(want, "\n") {
		wantLines[line] = true
	}
	gotLines := map[string]bool{}
	for _, line := range strings.Split(got, "\n") {
		gotLines[line] = true
	}
	var b strings.Builder
	for _, line := range strings.Split(want, "\n") {
		if !gotLines[line] {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if !wantLines[line] {
			fmt.Fprintf(&b, "+ %s\n", line)
		}
	}
	return b.String()
}

// The module analyzer's additions do nothing on the unmoved tree, so the fixture is the split's shape in miniature: a Client whose
// fields are *usecases.X embedding a port; a composite that only calls its port; a composite given ANOTHER family's port; a method
// value of a port passed to a generic helper (the Poller shape); a usecases method that overrides a port method; and an unwired
// second implementation of a port, in another adapter package, whose Kinds sends. Kinds must stay not sending: only rest is wired.
func TestModuleAnalyzerReadsTheHexagonalShape(t *testing.T) {
	graph := analyzeModuleRequestsIn(t, "testdata/hexshape")
	for symbol, wantVia := range map[string][]string{
		"Diffs.Count":           nil,
		"Diffs.FilesCount":      nil,
		"Diffs.MaterialSummary": {"Diffs.Count", "Diffs.FilesCount"},
		"Diffs.WaitCount":       {"Diffs.Count"},
		"Diffs.Summary":         nil,               // the usecases override calls rest's Summary, which is the same symbol
		"Diffs.Recount":         {"Diffs.Summary"}, // rest calls its own Summary, which a use case overrides: still Summary
		"Networks.List":         nil,
		"Reports.Compare":       {"Diffs.Count", "Networks.List"}, // cross-family: given the Diffs and Networks ports
	} {
		if !graph.issuing[symbol] {
			t.Errorf("%s is not seen to issue a request", symbol)
			continue
		}
		if got := strings.Join(graph.via[symbol], " "); got != strings.Join(wantVia, " ") {
			t.Errorf("%s reaches %q, want %q", symbol, got, strings.Join(wantVia, " "))
		}
	}
	for _, symbol := range []string{"Diffs.Kinds", "Networks.Bound"} {
		if graph.issuing[symbol] {
			t.Errorf("%s sends only in an unwired adapter, or not at all, but is counted as issuing a request", symbol)
		}
	}
	if len(graph.issuing) != 8 {
		t.Errorf("issuing = %v, want exactly the eight senders", graph.issuing)
	}
}

// recordingReporter collects what the analyzer reports, and stops it at the first Fatalf as testing.T would.
type recordingReporter struct{ messages []string }

type stopAnalysis struct{}

func (r *recordingReporter) Helper() {}
func (r *recordingReporter) Errorf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}
func (r *recordingReporter) Fatalf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
	panic(stopAnalysis{})
}

func analyzeExpectingProblems(t *testing.T, dir string) []string {
	t.Helper()
	problems, _ := analyzeExpectingProblemsGraph(t, dir)
	return problems
}

func analyzeExpectingProblemsGraph(t *testing.T, dir string) (problems []string, graph serviceRequestGraph) {
	t.Helper()
	r := &recordingReporter{}
	func() {
		defer func() {
			if p := recover(); p != nil {
				if _, ok := p.(stopAnalysis); !ok {
					panic(p)
				}
			}
		}()
		graph = analyzeModuleRequestsIn(r, dir)
	}()
	return r.messages, graph
}

// Each shape the analyzer cannot read must be reported, never skipped: a silent skip is a family, a method or a request the gate
// stops checking.
func TestModuleAnalyzerRefusesShapesItCannotRead(t *testing.T) {
	for dir, want := range map[string]string{
		"testdata/hexshape-bad/twoimpl":    "has 2 implementations in the adapter package",
		"testdata/hexshape-bad/noimpl":     "is promoted from",
		"testdata/hexshape-bad/ifacefield": "points to an interface",
		"testdata/hexshape-bad/valuefield": "not a pointer to a service type",
		"testdata/hexshape-bad/literal":    "an http.Request literal",
		"testdata/hexshape-bad/sender":     "sends a request the walk cannot see",
		"testdata/hexshape-bad/funcvalue":  "is referenced, not called",
		"testdata/hexshape-bad/callnoimpl": "which has no implementation in the adapter package; the walk cannot follow the call",
		"testdata/hexshape-bad/newreq":     "new(http.Request) builds a request",
		"testdata/hexshape-bad/varreq":     "a variable of type http.Request",
		"testdata/hexshape-bad/pkgfunc":    "is used outside a function declaration",
		"testdata/hexshape-bad/anoniface":  "calls (interface).Count, which has no implementation in the adapter package",
		"testdata/hexshape-bad/cgo":        "has cgo or build-constraint-excluded files",
		"testdata/hexshape-bad/excluded":   "has cgo or build-constraint-excluded files",
		"testdata/hexshape-bad/decorator":  "through an embedded interface",
	} {
		dir, want := dir, want
		t.Run(filepath.Base(dir), func(t *testing.T) {
			t.Parallel()
			problems := analyzeExpectingProblems(t, dir)
			found := false
			for _, problem := range problems {
				if strings.Contains(problem, want) {
					found = true
				}
			}
			if !found {
				t.Errorf("want a report containing %q; got %q", want, problems)
			}
		})
	}
	// An aliased field is read through the alias, not refused: it is the same type.
	t.Run("aliasfield", func(t *testing.T) {
		t.Parallel()
		problems, graph := analyzeExpectingProblemsGraph(t, "testdata/hexshape-bad/aliasfield")
		if len(problems) != 0 {
			t.Errorf("an aliased service field must be read through its alias; got %q", problems)
		}
		for _, symbol := range []string{"Diffs.Count", "Diffs.MaterialSummary", "Networks.List"} {
			if !graph.issuing[symbol] {
				t.Errorf("%s, on a field declared through an alias, is not seen to issue a request", symbol)
			}
		}
	})
}
