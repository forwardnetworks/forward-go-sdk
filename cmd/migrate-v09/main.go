// Command migrate-v09 rewrites a consumer's Go files for the hexagonal split of v0.9.0 (the v0.9 hexagonal split): every
// name the root package used to declare is rewritten to where the split put it, using a map generated from the move itself
// (map_gen.go). forward.Snapshot becomes domain.Snapshot, *forward.ErrorResponse becomes *ports.ErrorResponse,
// *forward.NetworksService becomes *usecases.Networks; names the root still declares (Client, Config, NewClient, ...) stay.
// It adds the imports it needs and drops the root import when nothing uses it any more. A root name the map does not know is
// reported with its position and left as it was, and the run exits 1, so nothing is changed without saying so.
//
//	go run github.com/forwardnetworks/forward-go-sdk/cmd/migrate-v09 [-prefer-ports] [-in-package] <dir>...
//
// -prefer-ports writes every moved name as ports.X, for a driving adapter that may import the root and ports but not domain;
// a name ports does not re-export is reported. -in-package migrates the SDK's own root package, whose files name the old
// root names without a qualifier: it resolves each with go/types (run it from the module root), so nothing is guessed.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const modulePath = "github.com/forwardnetworks/forward-go-sdk"

var (
	preferPorts  = flag.Bool("prefer-ports", false, "write moved names as ports.X (for a driving adapter)")
	inPackage    = flag.Bool("in-package", false, "migrate the SDK's own root package, resolving unqualified names with go/types")
	withTestdata = flag.Bool("testdata", false, "also migrate files under testdata/ (fixtures a test compiles); off by default, since a fixture may name old names on purpose")
)

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: migrate-v09 [-prefer-ports] [-in-package] <dir>...")
		os.Exit(2)
	}
	unmapped := 0
	for _, dir := range flag.Args() {
		migrate := migrateDir
		if *inPackage {
			migrate = migratePackage
		}
		n, err := migrate(dir, os.Stdout)
		if err != nil {
			fmt.Fprintln(os.Stderr, "migrate-v09:", err)
			os.Exit(1)
		}
		unmapped += n
	}
	if unmapped > 0 {
		fmt.Fprintf(os.Stdout, "%d names are not in the move map and were left unchanged\n", unmapped)
		os.Exit(1)
	}
}

// migrateDir rewrites every .go file under dir in place and returns how many names it could not map.
func migrateDir(dir string, report *os.File) (int, error) {
	unmapped := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != dir && (name == "vendor" || (name == "testdata" && !*withTestdata) || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, problems, err := migrateFile(path, src)
		if err != nil {
			return err
		}
		for _, p := range problems {
			fmt.Fprintln(report, p)
		}
		unmapped += len(problems)
		if !bytes.Equal(out, src) {
			return os.WriteFile(path, out, 0o644)
		}
		return nil
	})
	return unmapped, err
}

// movedPackages are the module's packages that changed import path at the split (step-zero amendment item 2).
var movedPackages = map[string]string{
	modulePath + "/perfpayload": modulePath + "/domain/perfpayload",
}

// rewriteImportPaths moves each import of a package that changed path, keeping the import's own name, so its uses read as before.
func rewriteImportPaths(path string, src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	pos := 0
	for _, is := range f.Imports {
		p, _ := strconv.Unquote(is.Path.Value)
		to, moved := movedPackages[p]
		if !moved {
			continue
		}
		start, end := fset.Position(is.Path.Pos()).Offset, fset.Position(is.Path.End()).Offset
		b.Write(src[pos:start])
		name := p[strings.LastIndex(p, "/")+1:]
		if is.Name == nil && name != to[strings.LastIndex(to, "/")+1:] {
			b.WriteString(name + " ")
		}
		b.WriteString(strconv.Quote(to))
		pos = end
	}
	if pos == 0 {
		return src, nil
	}
	b.Write(src[pos:])
	return format.Source(b.Bytes())
}

// migrateFile rewrites one file's references to the root package and its moved packages.
func migrateFile(path string, src []byte) ([]byte, []string, error) {
	src, err := rewriteImportPaths(path, src)
	if err != nil {
		return nil, nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	rootName := ""
	for _, is := range f.Imports {
		if p, _ := strconv.Unquote(is.Path.Value); p == modulePath {
			rootName = "forward"
			if is.Name != nil {
				rootName = is.Name.Name
			}
		}
	}
	if rootName == "" || rootName == "_" || rootName == "." {
		return src, nil, nil
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	var problems []string
	need := map[string]bool{}
	qualifier := layerQualifiers(f)
	rootStillUsed := false
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != rootName || x.Obj != nil { // x.Obj != nil: a local variable shadows the import
			return true
		}
		target, known := moveMap[sel.Sel.Name]
		switch {
		case !known:
			problems = append(problems, fmt.Sprintf("%s: %s.%s is not in the move map; left unchanged", fset.Position(sel.Pos()), rootName, sel.Sel.Name))
			rootStillUsed = true
		case target == "":
			rootStillUsed = true // the root still declares it
		default:
			pkg, name, _ := strings.Cut(target, ".")
			if *preferPorts && pkg == "domain" {
				if !portsNames[name] {
					problems = append(problems, fmt.Sprintf("%s: %s.%s moved to domain and ports does not re-export it; left unchanged", fset.Position(sel.Pos()), rootName, sel.Sel.Name))
					rootStillUsed = true
					return false
				}
				pkg = "ports"
			}
			need[pkg] = true
			edits = append(edits, edit{fset.Position(sel.Pos()).Offset, fset.Position(sel.End()).Offset, qualifier[pkg] + "." + name})
		}
		return false
	})
	if len(edits) == 0 {
		return src, problems, nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var b bytes.Buffer
	pos := 0
	for _, e := range edits {
		b.Write(src[pos:e.start])
		b.WriteString(e.text)
		pos = e.end
	}
	b.Write(src[pos:])
	out, err := fixImports(path, b.Bytes(), rootStillUsed, need, qualifier)
	if err != nil {
		return nil, nil, err
	}
	return out, problems, nil
}

// fixImports adds the layer packages the rewrite names and drops the root import if nothing names it any more.
func fixImports(path string, src []byte, rootStillUsed bool, need map[string]bool, qualifier map[string]string) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	var rootSpec *ast.ImportSpec
	for _, is := range f.Imports {
		p, _ := strconv.Unquote(is.Path.Value)
		have[p] = true
		if p == modulePath {
			rootSpec = is
		}
	}
	var adds []string
	for _, pkg := range []string{"domain", "ports", "usecases"} {
		if need[pkg] && !have[modulePath+"/"+pkg] {
			spec := strconv.Quote(modulePath + "/" + pkg)
			if qualifier[pkg] != pkg {
				spec = qualifier[pkg] + " " + spec
			}
			adds = append(adds, spec)
		}
	}
	start, end := fset.Position(rootSpec.Pos()).Offset, fset.Position(rootSpec.End()).Offset
	replacement := string(src[start:end])
	if !rootStillUsed {
		replacement = ""
	}
	if len(adds) > 0 {
		if replacement != "" {
			replacement += "\n\t"
		}
		replacement += strings.Join(adds, "\n\t")
	}
	var b bytes.Buffer
	b.Write(src[:start])
	b.WriteString(replacement)
	b.Write(src[end:])
	out := b.Bytes()
	if !isGrouped(f, rootSpec) && len(adds) > 0 {
		// A single-line `import forward "..."` became several specs: give them a group.
		out = bytes.Replace(out, []byte("import "+replacement), []byte("import (\n\t"+replacement+"\n)"), 1)
	}
	return format.Source(out)
}

func isGrouped(f *ast.File, spec *ast.ImportSpec) bool {
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			for _, s := range gd.Specs {
				if s == spec {
					return gd.Lparen.IsValid()
				}
			}
		}
	}
	return false
}

// layerQualifiers picks, for each layer package, the name a rewritten file calls it by: its own name, or the name the file
// already imports it under, or, when the file already uses that name for something else (a consumer's own package called
// domain), "fwd" plus the name, so a rewrite never resolves to the consumer's package.
func layerQualifiers(f *ast.File) map[string]string {
	taken := map[string]string{} // name -> import path
	for _, is := range f.Imports {
		p, _ := strconv.Unquote(is.Path.Value)
		name := p[strings.LastIndex(p, "/")+1:]
		if is.Name != nil {
			name = is.Name.Name
		}
		taken[name] = p
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				taken[d.Name.Name] = "(declared)"
			}
		case *ast.GenDecl:
			for _, sp := range d.Specs {
				switch sp := sp.(type) {
				case *ast.TypeSpec:
					taken[sp.Name.Name] = "(declared)"
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						taken[n.Name] = "(declared)"
					}
				}
			}
		}
	}
	out := map[string]string{}
	for _, pkg := range []string{"domain", "ports", "usecases"} {
		path := modulePath + "/" + pkg
		name := pkg
		for name2, p := range taken {
			if p == path {
				name = name2 // already imported, perhaps under another name
			}
		}
		if p, used := taken[name]; used && p != path {
			name = "fwd" + pkg
			for i := 2; taken[name] != ""; i++ {
				name = fmt.Sprintf("fwd%s%d", pkg, i)
			}
		}
		out[pkg] = name
	}
	return out
}
