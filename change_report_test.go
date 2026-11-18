package forward

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The golden files in testdata/change_report are shared byte for byte with forward-python-sdk and were written by hand from RULES.md, not from
// either implementation. Every case's input must assemble to exactly its report.json and render to exactly its report.md.
func TestChangeReportGoldens(t *testing.T) {
	cases, err := filepath.Glob("testdata/change_report/*/input.json")
	if err != nil || len(cases) == 0 {
		t.Fatalf("no golden cases found: %v", err)
	}
	for _, input := range cases {
		dir := filepath.Dir(input)
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			report, err := AssembleChangeReport(raw)
			if err != nil {
				t.Fatal(err)
			}
			wantJSON, _ := os.ReadFile(filepath.Join(dir, "report.json"))
			if got := report.JSON(); !bytes.Equal(got, wantJSON) {
				t.Errorf("report.json differs:\n%s", firstDifference(string(wantJSON), string(got)))
			}
			wantMD, _ := os.ReadFile(filepath.Join(dir, "report.md"))
			if got := report.Markdown(); got != string(wantMD) {
				t.Errorf("report.md differs:\n%s", firstDifference(string(wantMD), got))
			}
			// The renderer reads only report.json: rendering the parsed golden gives the same text.
			parsed, err := ParseChangeReport(wantJSON)
			if err != nil {
				t.Fatal(err)
			}
			if got := parsed.Markdown(); got != string(wantMD) {
				t.Errorf("rendering the parsed report.json differs:\n%s", firstDifference(string(wantMD), got))
			}
		})
	}
}

// The directory is a byte-identical copy of forward-python-sdk's, pinned by a sha256sum-style manifest.
func TestChangeReportManifest(t *testing.T) {
	manifest, err := os.ReadFile("testdata/change_report/MANIFEST.sha256")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(manifest), "\n"), "\n")
	if !sort.StringsAreSorted(lines) && !sortedByPath(lines) {
		t.Error("the manifest lines are not sorted by path")
	}
	listed := map[string]bool{}
	for _, line := range lines {
		sum, path, ok := strings.Cut(line, "  ")
		if !ok {
			t.Fatalf("bad manifest line %q", line)
		}
		data, err := os.ReadFile(filepath.Join("testdata/change_report", path))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != sum {
			t.Errorf("%s does not match the pinned digest", path)
		}
		listed[path] = true
	}
	// Every fixture file is pinned: nothing can be added or changed without a manifest bump.
	_ = filepath.Walk("testdata/change_report", func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel("testdata/change_report", p)
			if rel != "MANIFEST.sha256" && !listed[rel] {
				t.Errorf("%s is not in the manifest", rel)
			}
		}
		return nil
	})
}

func sortedByPath(lines []string) bool {
	paths := make([]string, len(lines))
	for i, l := range lines {
		_, p, _ := strings.Cut(l, "  ")
		paths[i] = p
	}
	return sort.StringsAreSorted(paths)
}

func firstDifference(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			return "line " + itoa(i+1) + "\n  want: " + w + "\n  got:  " + g
		}
	}
	return "(no line difference: trailing bytes differ)"
}

func itoa(n int) string { return strconv.Itoa(n) }
