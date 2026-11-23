package forward

import (
	"flag"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// updateClientSignatures records the signatures of methods added since the hexagonal split (the v0.9 hexagonal split) in
// testdata/client_signatures_added.txt. testdata/client_signatures.txt was recorded on the unmoved tree and is never rewritten.
var updateClientSignatures = flag.Bool("update-client-signatures", false, "record added methods in testdata/client_signatures_added.txt")

const clientSignaturesPath = "testdata/client_signatures.txt"

// signatureMoves are the approved differences: the types the split moves out of the root (the placement table sends each to
// usecases), written as they render after the move and as they render before it. A signature that differs in any other way is
// a changed API, which the split must not make.
var signatureMoves = map[string]string{
	"usecases.Poller[":             "forward.Poller[",
	"usecases.CollectionOperation": "forward.CollectionOperation",
	"usecases.SnapshotOperation":   "forward.SnapshotOperation",
}

// Every exported method of every Client field keeps its exact signature across the split: same parameter and result types,
// in order, with aliases resolved (reflect sees through them), and the same variadic flag. The wire goldens call each method
// with synthesised arguments, so they cannot see a parameter or result type that changed; this can.
func TestClientMethodSignaturesAreUnchanged(t *testing.T) {
	rendered := renderClientSignatures()
	if n := strings.Count(rendered, "\n"); n < 795 {
		t.Fatalf("only %d signatures rendered; the check proves little", n)
	}
	if *updateClientSignatures {
		writeAddedLines(t, clientSignaturesPath, rendered, sameText) // the frozen file is never rewritten
		return
	}
	want := readLineReference(t, clientSignaturesPath)
	wantLines, gotLines := lineSet(string(want)), lineSet(rendered)
	var diffs []string
	for line := range wantLines {
		if !gotLines[line] {
			diffs = append(diffs, "- "+line)
		}
	}
	for line := range gotLines {
		if !wantLines[line] {
			diffs = append(diffs, "+ "+line)
		}
	}
	sort.Strings(diffs)
	if len(diffs) != 0 {
		t.Errorf("%d signature lines differ from %s:\n%s", len(diffs), clientSignaturesPath, strings.Join(diffs, "\n"))
	}
}

func renderClientSignatures() string {
	ct := reflect.TypeOf(Client{})
	var lines []string
	for i := 0; i < ct.NumField(); i++ {
		field := ct.Field(i)
		if !field.IsExported() || field.Type.Kind() != reflect.Pointer {
			continue
		}
		for j := 0; j < field.Type.NumMethod(); j++ {
			m := field.Type.Method(j)
			if !m.IsExported() {
				continue
			}
			mt := m.Type // the receiver is In(0)
			var in, out []string
			for k := 1; k < mt.NumIn(); k++ {
				arg := mt.In(k).String()
				if mt.IsVariadic() && k == mt.NumIn()-1 {
					arg = "..." + mt.In(k).Elem().String()
				}
				in = append(in, arg)
			}
			for k := 0; k < mt.NumOut(); k++ {
				out = append(out, mt.Out(k).String())
			}
			line := fmt.Sprintf("%s.%s(%s) (%s)", field.Name, m.Name, strings.Join(in, ", "), strings.Join(out, ", "))
			for after, before := range signatureMoves {
				line = strings.ReplaceAll(line, after, before)
			}
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func lineSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		out[line] = true
	}
	return out
}
