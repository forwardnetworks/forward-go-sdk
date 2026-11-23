package forward

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// The references the hexagonal split was proven against (the wire golden, the request graph, the signatures, the nil-service
// answers) were recorded on the unmoved tree, and two of them are pinned by hash: they are never rewritten. A symbol added
// after the split has no line in them, so its reference lives in a companion file, <name>_added.txt, beside the frozen one. A
// check compares the tree with the union of the two. The frozen file still proves every old symbol unchanged; the companion
// is the reviewable record of what was added since. A symbol may appear in only one of the two.

// addedPath is the companion of a frozen reference.
func addedPath(frozen string) string { return strings.TrimSuffix(frozen, ".txt") + "_added.txt" }

// lineKey is the symbol a reference line is about: its first token, up to a space or an opening parenthesis.
func lineKey(line string) string {
	if i := strings.IndexAny(line, " ("); i >= 0 {
		return line[:i]
	}
	return line
}

// readLineReference returns the frozen reference and its companion, merged and sorted. A symbol in both is an error.
func readLineReference(t *testing.T, frozen string) []byte {
	t.Helper()
	base, err := os.ReadFile(frozen)
	if err != nil {
		t.Fatalf("%v (the frozen reference is recorded once, on the unmoved tree)", err)
	}
	added, err := os.ReadFile(addedPath(frozen))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var lines []string
	for _, line := range splitLines(string(base)) {
		seen[lineKey(line)] = true
		lines = append(lines, line)
	}
	for _, line := range splitLines(string(added)) {
		if seen[lineKey(line)] {
			t.Errorf("%s names %s, which the frozen %s already holds", addedPath(frozen), lineKey(line), frozen)
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// writeAddedLines records, in the companion, the rendered lines of every symbol the frozen reference does not hold. It refuses,
// writing nothing, when a symbol the frozen reference holds renders differently or is gone: that is a change to recorded
// behaviour, never an addition.
func writeAddedLines(t *testing.T, frozen, rendered string, same func(want, got string) bool) {
	t.Helper()
	base, err := os.ReadFile(frozen)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, line := range splitLines(rendered) {
		got[lineKey(line)] = append(got[lineKey(line)], line)
	}
	want := map[string][]string{}
	for _, line := range splitLines(string(base)) {
		want[lineKey(line)] = append(want[lineKey(line)], line)
	}
	for key, lines := range want {
		if !same(strings.Join(lines, "\n"), strings.Join(got[key], "\n")) {
			t.Errorf("%s: %s changed (%q became %q); the frozen reference is not rewritten", frozen, key, lines, got[key])
		}
	}
	if t.Failed() {
		return
	}
	var added []string
	for key, lines := range got {
		if _, frozenHas := want[key]; !frozenHas {
			added = append(added, lines...)
		}
	}
	sort.Strings(added)
	out := ""
	if len(added) > 0 {
		out = strings.Join(added, "\n") + "\n"
	}
	if err := os.WriteFile(addedPath(frozen), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s: %d lines for symbols added since the split", addedPath(frozen), len(added))
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func sameText(want, got string) bool { return want == got }
