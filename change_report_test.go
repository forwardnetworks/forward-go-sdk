package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

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
			report, err := domain.AssembleChangeReport(raw)
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
			parsed, err := domain.ParseChangeReport(wantJSON)
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

// RULES.md "Input errors": a duplicate identity anywhere, a missing or unknown flow expectation, a bad ai_summary.text, a predicted-only section
// on a collected after (or missing on a predicted one), and a non-terminal check status are errors, never a report.
func TestChangeReportInputErrors(t *testing.T) {
	base := func(after string, sections string) string {
		return `{"meta":{"base_snapshot_id":"1","after_snapshot_id":"2","after_kind":"` + after + `","change_set_id":null,"network_id":"n"},
		"options":{"created_at":"2026-01-01T00:00:00Z","sdk":{"name":"x","version":"1"}},
		"sections":{"checks":{"status":"ok","items":[]},"connectivity":{"status":"ok","evaluated_subnet_pairs":0,"total_subnet_pairs":0,"location_pairs":{"ADDED":[],"DELETED":[],"MODIFIED":[]}},
		"routing":{"status":"ok","items":[]},"config":{"status":"ok","items":[]},"devices":{"status":"ok","added":[],"removed":[],"modified":[]},
		"diff_counts":{"status":"ok","families":{}},"vulnerabilities":{"status":"ok","introduced":0,"resolved":0,"exposed_devices":null}` + sections + `}}`
	}
	replace := func(doc, section, with string) string {
		start := strings.Index(doc, `"`+section+`":`)
		if start < 0 {
			t.Fatalf("no %s section in the template", section)
		}
		depth, end := 0, start
		for i := start; i < len(doc); i++ {
			switch doc[i] {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					end = i + 1
					i = len(doc)
				}
			}
		}
		return doc[:start] + `"` + section + `":` + with + doc[end:]
	}
	check := func(id, before, after string) string {
		return `{"check_id":"` + id + `","name":"n","type":"Existential","base_status":"` + before + `","after_status":"` + after + `"}`
	}
	flow := func(extra string) string { return `{"name":"f"` + extra + `}` }
	counts := `{"blackhole":0,"dropped":0,"loop":0,"other":0,"unreachable":0,"valid":1}`
	cases := map[string]string{
		"duplicate check id":      replace(base("collected", ""), "checks", `{"status":"ok","items":[`+check("C-1", "PASS", "PASS")+`,`+check("C-1", "PASS", "FAIL")+`]}`),
		"still-computing status":  replace(base("collected", ""), "checks", `{"status":"ok","items":[`+check("C-1", "PASS", "PROCESSING")+`]}`),
		"duplicate routing row":   replace(base("collected", ""), "routing", `{"status":"ok","items":[{"device":"a","added":1,"removed":0,"modified":0},{"device":"a","added":0,"removed":1,"modified":0}]}`),
		"duplicate device name":   replace(base("collected", ""), "devices", `{"status":"ok","added":["a","a"],"removed":[],"modified":[]}`),
		"missing expectation":     base("collected", `,"flows":{"status":"ok","items":[`+flow(`,"base":`+counts+`,"after":`+counts)+`]}`),
		"unknown expectation":     base("collected", `,"flows":{"status":"ok","items":[`+flow(`,"expectation":"sometimes","base":`+counts+`,"after":`+counts)+`]}`),
		"duplicate flow":          base("collected", `,"flows":{"status":"ok","items":[`+flow(`,"expectation":"any","base":`+counts+`,"after":`+counts)+`,`+flow(`,"expectation":"any","base":`+counts+`,"after":`+counts)+`]}`),
		"empty ai summary":        base("collected", `,"ai_summary":{"status":"ok","text":""}`),
		"newline ai summary":      base("collected", `,"ai_summary":{"status":"ok","text":"x\n"}`),
		"predict on collected":    base("collected", `,"predict":{"status":"ok","processing_state":"PROCESSED","failures":[],"modeling_problems":[]}`),
		"predicted missing":       base("predicted", ""),
		"duplicate state item":    base("collected", `,"state":{"status":"ok","base":[{"kind":"bgp_session","identity":["a","b","c"],"value":"ESTABLISHED"},{"kind":"bgp_session","identity":["a","b","c"],"value":"IDLE"}],"after":[]}`),
		"duplicate location pair": strings.Replace(base("collected", ""), `"DELETED":[]`, `"DELETED":[{"src":"a","dst":"b","count":1},{"src":"a","dst":"b","count":2}]`, 1),
		"unknown section status":  replace(base("collected", ""), "routing", `{"status":"maybe"}`),
	}
	for name, input := range cases {
		if _, err := domain.AssembleChangeReport([]byte(input)); err == nil {
			t.Errorf("%s: expected an input error", name)
		}
	}
	// The same shapes without the defect assemble: an `any` flow that loses its path never drives the verdict.
	lost := `{"blackhole":0,"dropped":1,"loop":0,"other":0,"unreachable":0,"valid":0}`
	ok := base("collected", `,"flows":{"status":"ok","items":[`+flow(`,"expectation":"any","base":`+counts+`,"after":`+lost)+`]}`)
	report, err := domain.AssembleChangeReport([]byte(ok))
	if err != nil || report.Verdict() != domain.ChangeReportNoRegression {
		t.Fatalf("an `any` flow must not drive the verdict: %v %v", err, report.Verdict())
	}
	valid := base("collected", `,"flows":{"status":"ok","items":[`+flow(`,"expectation":"valid","base":`+counts+`,"after":`+lost)+`]}`)
	if report, err := domain.AssembleChangeReport([]byte(valid)); err != nil || report.Verdict() != domain.ChangeReportRegression {
		t.Fatalf("a `valid` flow that loses its path regresses: %v", err)
	}
}
