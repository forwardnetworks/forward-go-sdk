package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// aliasFile is where the root package re-exported the names that moved; -in-package rewrites every other file's uses of them.
const aliasFile = "domain_aliases.go"

// migratePackage migrates the root package in dir in place: every unqualified use, in any file but the alias file, of a name the
// alias file declares becomes the domain or ports name it re-exports. The package's external test files (package forward_test)
// are migrated like any consumer.
func migratePackage(dir string, report *os.File) (int, error) {
	fset := token.NewFileSet()
	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return 0, err
	}
	sort.Strings(names)
	var files []*ast.File
	paths := map[*ast.File]string{}
	unmapped := 0
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			return 0, err
		}
		if f.Name.Name != "forward" {
			src, err := os.ReadFile(name)
			if err != nil {
				return 0, err
			}
			out, problems, err := migrateFile(name, src)
			if err != nil {
				return 0, err
			}
			for _, p := range problems {
				fmt.Fprintln(report, p)
			}
			unmapped += len(problems)
			if !bytes.Equal(out, src) {
				if err := os.WriteFile(name, out, 0o644); err != nil {
					return 0, err
				}
			}
			continue
		}
		files = append(files, f)
		paths[f] = name
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	pkg, err := (&types.Config{Importer: importer.ForCompiler(fset, "source", nil)}).Check(modulePath, fset, files, info)
	if err != nil {
		return 0, fmt.Errorf("type-check %s: %w", dir, err)
	}
	for _, f := range files {
		path := paths[f]
		if filepath.Base(path) == aliasFile {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return 0, err
		}
		type edit struct {
			start, end int
			text       string
		}
		var edits []edit
		need := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			o := info.Uses[id]
			if o == nil || o.Pkg() != pkg || o.Parent() != pkg.Scope() || filepath.Base(fset.Position(o.Pos()).Filename) != aliasFile {
				return true
			}
			target, known := moveMap[o.Name()]
			if !known || target == "" {
				fmt.Fprintf(report, "%s: %s is declared in %s but not mapped to domain or ports; left unchanged\n", fset.Position(id.Pos()), o.Name(), aliasFile)
				unmapped++
				return true
			}
			p, _, _ := strings.Cut(target, ".")
			need[p] = true
			edits = append(edits, edit{fset.Position(id.Pos()).Offset, fset.Position(id.End()).Offset, target})
			return true
		})
		if len(edits) == 0 {
			continue
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
		out, err := addImports(path, b.Bytes(), need)
		if err != nil {
			return 0, err
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return 0, err
		}
	}
	return unmapped, nil
}

// addImports adds the layer packages a rewritten file now names, in a new import block after the package clause; gofmt merges
// nothing, so a file keeps its own block and gains one more.
func addImports(path string, src []byte, need map[string]bool) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, is := range f.Imports {
		p, _ := strconv.Unquote(is.Path.Value)
		have[p] = true
	}
	var adds []string
	for _, p := range []string{"domain", "ports"} {
		if need[p] && !have[modulePath+"/"+p] {
			adds = append(adds, strconv.Quote(modulePath+"/"+p))
		}
	}
	if len(adds) == 0 {
		return format.Source(src)
	}
	at := fset.Position(f.Name.End()).Offset
	if i := bytes.IndexByte(src[at:], '\n'); i >= 0 {
		at += i + 1
	}
	block := "\nimport (\n\t" + strings.Join(adds, "\n\t") + "\n)\n"
	out := append(append(append([]byte{}, src[:at]...), []byte(block)...), src[at:]...)
	return format.Source(out)
}
