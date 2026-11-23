package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// updateWireGolden records the requests of symbols added since the hexagonal split (the v0.9 hexagonal split) in
// testdata/wire_golden_added.txt. testdata/wire_golden.txt was recorded at v0.8.59 and is pinned by hash: it is never rewritten,
// and an update refuses if a recorded symbol's requests changed.
var updateWireGolden = flag.Bool("update-wire-golden", false, "record symbols added since the split in testdata/wire_golden_added.txt")

const wireGoldenPath = "testdata/wire_golden.txt"

// wireGoldenSHA256 pins the frozen golden recorded at v0.8.59. A change to what any v0.8.59 symbol sends must come with a new
// golden and a new pin in the same reviewed commit; additions go in testdata/wire_golden_added.txt instead, which is not pinned.
const wireGoldenSHA256 = "228a5a4b11c849e265a0e1511dc0caf0aab01fefa35a51d313ab57ca9d03963a"

// TestWireGolden is the differential oracle for the hexagonal split (the v0.9 hexagonal split): every manifest symbol, called with the same
// synthesised arguments TestCoverageManifestRoutesMatchTheWire uses, must send exactly the requests it sent at the reference commit:
// method, path, query, the headers that carry meaning, and the body. A refactor that changes any of them changes what Forward sees.
//
// Each symbol's requests are a sorted set, not a sequence: a poller repeats a request a number of times that depends on timing, and a
// composite may send in parallel. Order and repetition are therefore not compared, which is this oracle's stated blind spot.
func TestWireGolden(t *testing.T) {
	// Synthesised times render in the local zone (an AI-chat "since" carried -05:00); pin it so the golden is the same on every machine.
	defer func(local *time.Location) { time.Local = local }(time.Local)
	time.Local = time.UTC

	symbols := make([]string, 0, len(sdkCoverageCatalog))
	for symbol := range sdkCoverageCatalog {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	got := make(map[string][]string, len(symbols))
	var mu sync.Mutex
	t.Run("record", func(t *testing.T) {
		for _, symbol := range symbols {
			symbol := symbol
			t.Run(symbol, func(t *testing.T) {
				t.Parallel()
				requests := recordWireTranscript(t, symbol)
				mu.Lock()
				got[symbol] = requests
				mu.Unlock()
			})
		}
	})
	rendered := renderWireGolden(symbols, got)

	// A section with no request compares equal to another empty one, so it proves nothing: every symbol that sends nothing with
	// synthesised arguments must be named here, and a symbol named here that starts sending must be taken off the list.
	for _, symbol := range symbols {
		_, listed := wireGoldenSilent[symbol]
		switch empty := len(got[symbol]) == 0; {
		case empty && !listed:
			t.Errorf("%s sent no request, so the golden cannot check it; give it an argument override or list it in wireGoldenSilent", symbol)
		case !empty && listed:
			t.Errorf("%s now sends requests; take it off wireGoldenSilent", symbol)
		}
	}
	for symbol := range wireGoldenSilent {
		if _, known := sdkCoverageCatalog[symbol]; !known {
			t.Errorf("wireGoldenSilent names %s, which is not a manifest symbol", symbol)
		}
	}

	frozen, err := os.ReadFile(wireGoldenPath)
	if err != nil {
		t.Fatalf("%v (the golden is recorded once, at the reference commit)", err)
	}
	if sum := sha256.Sum256(frozen); hex.EncodeToString(sum[:]) != wireGoldenSHA256 {
		t.Fatalf("%s has SHA-256 %x, not the pinned %s: the frozen wire golden was rewritten", wireGoldenPath, sum, wireGoldenSHA256)
	}
	if *updateWireGolden {
		writeAddedWireSections(t, string(frozen), rendered)
		return
	}
	want := mergeWireSections(t, string(frozen))
	if diff := wireGoldenDiff(want, rendered); diff != "" {
		t.Errorf("the wire differs from %s and %s:\n%s", wireGoldenPath, addedPath(wireGoldenPath), diff)
	}
}

// wireSections splits a golden into its sections by symbol, each with its "== symbol" header.
func wireSections(s string) (map[string]string, []string) {
	out := map[string]string{}
	var order []string
	var name string
	for _, line := range strings.SplitAfter(s, "\n") {
		if strings.HasPrefix(line, "== ") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "== "))
			order = append(order, name)
		}
		if name != "" {
			out[name] += line
		}
	}
	return out, order
}

// mergeWireSections is the frozen golden plus its companion, in symbol order; a symbol in both is an error.
func mergeWireSections(t *testing.T, frozen string) string {
	t.Helper()
	added, err := os.ReadFile(addedPath(wireGoldenPath))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	all, _ := wireSections(frozen)
	more, _ := wireSections(string(added))
	for name, section := range more {
		if _, dup := all[name]; dup {
			t.Errorf("%s names %s, which the frozen %s already holds", addedPath(wireGoldenPath), name, wireGoldenPath)
		}
		all[name] = section
	}
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(all[name])
	}
	return b.String()
}

// writeAddedWireSections records the sections of symbols the frozen golden does not hold, refusing (and writing nothing) if a
// recorded symbol's requests changed or it is gone.
func writeAddedWireSections(t *testing.T, frozen, rendered string) {
	t.Helper()
	want, _ := wireSections(frozen)
	got, order := wireSections(rendered)
	for name, section := range want {
		if got[name] != section {
			t.Errorf("%s: %s changed; the frozen golden is not rewritten", wireGoldenPath, name)
		}
	}
	if t.Failed() {
		return
	}
	var b strings.Builder
	n := 0
	for _, name := range order {
		if _, recorded := want[name]; !recorded {
			b.WriteString(got[name])
			n++
		}
	}
	if err := os.WriteFile(addedPath(wireGoldenPath), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s: %d symbols added since the split", addedPath(wireGoldenPath), n)
}

func renderWireGolden(symbols []string, got map[string][]string) string {
	var b strings.Builder
	for _, symbol := range symbols {
		fmt.Fprintf(&b, "== %s\n", symbol)
		for _, request := range got[symbol] {
			b.WriteString(request)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// wireGoldenDiff names each symbol whose section differs, with both sections.
func wireGoldenDiff(want, got string) string {
	sections := func(s string) map[string]string {
		out := map[string]string{}
		var name string
		for _, line := range strings.SplitAfter(s, "\n") {
			if strings.HasPrefix(line, "== ") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "== "))
				out[name] = ""
				continue
			}
			out[name] += line
		}
		return out
	}
	w, g := sections(want), sections(got)
	names := map[string]bool{}
	for name := range w {
		names[name] = true
	}
	for name := range g {
		names[name] = true
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	var b strings.Builder
	for _, name := range sorted {
		wantSection, inWant := w[name]
		gotSection, inGot := g[name]
		switch {
		case !inWant:
			fmt.Fprintf(&b, "+ %s is new:\n%s", name, gotSection)
		case !inGot:
			fmt.Fprintf(&b, "- %s is gone; it sent:\n%s", name, wantSection)
		case wantSection != gotSection:
			fmt.Fprintf(&b, "~ %s\n  want:\n%s  got:\n%s", name, indent(wantSection), indent(gotSection))
		}
	}
	return b.String()
}

func indent(s string) string {
	if s == "" {
		return "    (no request)\n"
	}
	return "    " + strings.ReplaceAll(strings.TrimSuffix(s, "\n"), "\n", "\n    ") + "\n"
}

// recordWireTranscript calls the symbol under every principal mode and returns all their canonical requests. The mode is part of
// each line, so a method that starts refusing (or accepting) a principal shows up.
func recordWireTranscript(t *testing.T, symbol string) []string {
	t.Helper()
	var out []string
	for _, mode := range []domain.AuthMode{"", domain.AuthModeService, domain.AuthModeBrowser, domain.AuthModeNone} {
		out = append(out, recordWireTranscriptAs(t, symbol, mode)...)
	}
	return out
}

var multipartBoundary = regexp.MustCompile(`boundary=([0-9A-Za-z'()+_,\-./:=?]+)`)

func recordWireTranscriptAs(t *testing.T, symbol string, mode domain.AuthMode) []string {
	t.Helper()
	var mu sync.Mutex
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		line := canonicalWireRequest(mode, r, body)
		mu.Lock()
		seen[line] = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	cfg := Config{BaseURL: server.URL, Username: "user", Password: "pass", NetworkID: "net1", AuthMode: mode, HTTPClient: privateHTTPClient()}
	switch mode {
	case domain.AuthModeBrowser:
		cfg.Username, cfg.Password = "", ""
		cfg.Cookies = []*http.Cookie{{Name: "SESSION", Value: "v1"}, {Name: "XSRF-TOKEN", Value: "v1"}}
	case domain.AuthModeNone:
		cfg.Username, cfg.Password = "", ""
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("%s: NewClient(%q) error = %v", symbol, mode, err)
	}
	callManifestSymbol(t, client, symbol)

	mu.Lock()
	defer mu.Unlock()
	out := make([]string, 0, len(seen))
	for line := range seen {
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}

// callManifestSymbol invokes Client.<Service>.<Method> with the arguments recordRequestsAs would give it.
func callManifestSymbol(t *testing.T, client *Client, symbol string) {
	t.Helper()
	serviceName, methodName, _ := strings.Cut(symbol, ".")
	service := reflect.ValueOf(client).Elem().FieldByName(serviceName)
	if !service.IsValid() {
		t.Fatalf("%s: no Client field %s", symbol, serviceName)
	}
	method := service.MethodByName(methodName)
	if !method.IsValid() {
		t.Fatalf("%s: no method %s on %s", symbol, methodName, service.Type())
	}
	// Wide, because a missed deadline drops a request from the golden: under -race with the package's other tests running, 300ms
	// was too short for the first mode's request. A poller polls until it, and the recorded set is the same.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	args := make([]reflect.Value, method.Type().NumIn())
	for i := range args {
		in := method.Type().In(i)
		switch override, ok := wireArgOverrides[symbol][i]; {
		case ok:
			args[i] = reflect.ValueOf(wireArgValue(override)).Convert(in)
		case in == reflect.TypeOf((*context.Context)(nil)).Elem():
			args[i] = reflect.ValueOf(ctx)
		default:
			args[i] = synthesize(in, 0)
		}
	}
	func() {
		defer func() { _ = recover() }()
		if method.Type().IsVariadic() {
			method.CallSlice(args) // the synthesiser built the variadic argument as a slice
		} else {
			method.Call(args)
		}
	}()
}

// canonicalWireRequest renders one request as a line that is the same on every run. The multipart boundary is replaced. Credentials
// are recorded whole (they are the test's own fakes), so sending another principal's credential is a difference.
func canonicalWireRequest(mode domain.AuthMode, r *http.Request, body []byte) string {
	auth := r.Header.Get("Authorization")
	var cookies []string
	for _, c := range r.Cookies() {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	sort.Strings(cookies)
	auth = fmt.Sprintf("%q cookies=%q xsrf=%q ua=%q", auth, strings.Join(cookies, ";"), r.Header.Get("X-XSRF-TOKEN"), r.Header.Get("User-Agent"))
	contentType := r.Header.Get("Content-Type")
	if mediaType, params, err := mime.ParseMediaType(contentType); err == nil {
		if boundary := params["boundary"]; boundary != "" {
			body = bytes.ReplaceAll(body, []byte(boundary), []byte("BOUNDARY"))
			contentType = multipartBoundary.ReplaceAllString(contentType, "boundary=BOUNDARY")
		}
		_ = mediaType
	}
	query := r.URL.RawQuery
	if query != "" {
		query = "?" + query
	}
	return fmt.Sprintf("mode=%q %s %s%s auth=%s accept=%q content-type=%q body=%q",
		string(mode), r.Method, r.URL.EscapedPath(), query, auth, r.Header.Get("Accept"), contentType, string(body))
}

// wireGoldenSilent names the manifest symbols that send no request with synthesised arguments, so TestWireGolden cannot check
// them; the black-box tests of each family remain their oracle. Shrink it with wireArgOverrides; never grow it to pass.
var wireGoldenSilent = map[string]struct{}{
	"Browser.ImpersonateWithServiceCredential": {},
	"Browser.Login":                       {},
	"Browser.SAMLAssertionConsumer":       {},
	"ChangeReports.Compare":               {},
	"CollectorTasks.Stop":                 {},
	"Credentials.CreateSNMPCredential":    {},
	"CustomCommandGroups.Update":          {},
	"CVEIndex.Put":                        {},
	"Diffs.ConnectivityDiffLocationPairs": {},
	"NQE.DiffSource":                      {},
	"NQE.Run":                             {},
	"NQE.Start":                           {},
	"NQE.StartOperation":                  {},
	"Snapshots.ExportSubset":              {},
	"Snapshots.ExportSubsetCompatibility": {},
	"Snapshots.MergeFileIntoLatest":       {},
	"Snapshots.ResolveID":                 {},
	"Trace.ExplainQuery":                  {},
	"Trace.TraceFor":                      {},
	"Vulnerabilities.DetectionLogic":      {},
	"Webhooks.Create":                     {},
	"Webhooks.TestNew":                    {},
}
