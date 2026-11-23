package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// -prefer-ports writes a moved name as ports.X only if ports declares X, and decides that from portsNames in map_gen.go. A
// stale list reports a name that ports does re-export as unmapped (or, worse, writes a ports name that does not exist). The list
// must therefore equal the exported package-level names of the ports package as it stands, so growing ports without updating it fails here.
func TestPortsNamesEqualThePortsPackagesExports(t *testing.T) {
	files, err := filepath.Glob("../../ports/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no ports sources found: %v", err)
	}
	declared := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					declared[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if spec.Name.IsExported() {
							declared[spec.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, n := range spec.Names {
							if n.IsExported() {
								declared[n.Name] = true
							}
						}
					}
				}
			}
		}
	}
	var missing, extra []string
	for n := range declared {
		if !portsNames[n] {
			missing = append(missing, n)
		}
	}
	for n := range portsNames {
		if !declared[n] {
			extra = append(extra, n)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) != 0 || len(extra) != 0 {
		t.Errorf("portsNames in map_gen.go is out of date with package ports:\n  ports declares, the list lacks: %v\n  the list names, ports does not declare: %v", missing, extra)
	}
	if len(declared) < 500 {
		t.Errorf("only %d ports names read; the check proves little", len(declared))
	}
}
