package forward

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCoverageManifestRoutesMatchTheWire calls every manifest symbol against
// a recording server and fails when the method sends a request the manifest
// does not declare for it.
//
// TestCoverageManifestIsComplete proves a method issues SOME request; it
// cannot see WHICH. A wrong route in the manifest therefore passed every
// check: NQE.ListQueries was declared as GET
// /api/nqe/repos/fwd/commits/head/queries from the first commit while it
// sent GET /api/nqe/queries, so Skyforge's route-liveness gate spent that
// whole time checking a route the method never calls.
//
// Arguments are synthesised by reflection (non-empty strings, one-element
// slices, filled structs). A method whose validation rejects them sends
// nothing and is skipped rather than failed -- the check is "everything sent
// is declared", which a wrong manifest route cannot survive once the method
// is exercised. minExercised stops that escape hatch from silently widening.
func TestCoverageManifestRoutesMatchTheWire(t *testing.T) {
	const minExercised = 329

	symbols := make([]string, 0, len(sdkCoverageCatalog))
	for symbol := range sdkCoverageCatalog {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	var mu sync.Mutex
	exercised := 0
	t.Run("symbols", func(t *testing.T) {
		for _, symbol := range symbols {
			symbol := symbol
			t.Run(symbol, func(t *testing.T) {
				t.Parallel()
				sent := recordRequests(t, symbol)
				if len(sent) == 0 {
					t.Skip("synthesised arguments sent no request")
				}
				mu.Lock()
				exercised++
				mu.Unlock()
				declared := sdkCoverageCatalog[symbol]
				for _, request := range sent {
					if !routeDeclared(declared, request.method, request.path) {
						t.Errorf("%s sends %s %s, which the manifest does not declare for it (declared: %v)",
							symbol, request.method, request.path, declared)
					}
				}
			})
		}
	})
	if exercised < minExercised {
		t.Errorf("only %d of %d manifest symbols sent a request with synthesised arguments; want at least %d",
			exercised, len(symbols), minExercised)
	}
	t.Logf("%d of %d manifest symbols exercised", exercised, len(symbols))
}

type wireRequest struct{ method, path string }

// wireArgOverrides pins arguments the synthesiser cannot guess: values the
// method validates against an enum, and dynamic segments the manifest pins to
// the one value its consumer sends. Keyed by symbol, then argument index
// (index 0 is the context).
var wireArgOverrides = map[string]map[int]any{
	"Diffs.Count":                  {3: DiffDevices},
	"Properties.ClearOrganization": {2: OrgProperty("software_central")},
	"Users.SetNetworkRole":         {3: NetworkRoleAdmin},
	"Users.AddNetworkRole":         {3: NetworkRoleAdmin},
	"Users.RemoveNetworkRole":      {3: NetworkRoleAdmin},
	// The kind-based synthetic node calls address a different route per
	// kind; L3 VPN reaches every one of them.
	"SyntheticNodes.Get":               {2: SyntheticL3VPN},
	"SyntheticNodes.Put":               {2: SyntheticL3VPN},
	"SyntheticNodes.Delete":            {2: SyntheticL3VPN},
	"SyntheticNodes.List":              {2: SyntheticL3VPN},
	"SyntheticNodes.SetQuery":          {2: SyntheticL3VPN},
	"SyntheticNodes.ComputeQuery":      {2: SyntheticL3VPN},
	"SyntheticNodes.CompatibleQueries": {2: SyntheticL3VPN},
	"SyntheticNodes.Backdate":          {2: SyntheticL3VPN},
}

// recordRequests invokes symbol and returns every request it sent. Each
// principal mode is tried in turn, because a service-only or browser-only
// method refuses a user client before it reaches the wire.
func recordRequests(t *testing.T, symbol string) []wireRequest {
	t.Helper()
	for _, mode := range []AuthMode{"", AuthModeService, AuthModeBrowser, AuthModeNone} {
		if sent := recordRequestsAs(t, symbol, mode); len(sent) != 0 {
			return sent
		}
	}
	return nil
}

func recordRequestsAs(t *testing.T, symbol string, mode AuthMode) []wireRequest {
	t.Helper()
	var mu sync.Mutex
	var sent []wireRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sent = append(sent, wireRequest{r.Method, r.URL.EscapedPath()})
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	cfg := Config{BaseURL: server.URL, Username: "user", Password: "pass", NetworkID: "net1", AuthMode: mode}
	switch mode {
	case AuthModeBrowser:
		cfg.Username, cfg.Password = "", ""
		cfg.Cookies = []*http.Cookie{{Name: "SESSION", Value: "v1"}, {Name: "XSRF-TOKEN", Value: "v1"}}
	case AuthModeNone:
		cfg.Username, cfg.Password = "", ""
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("%s: NewClient(%q) error = %v", symbol, mode, err)
	}
	serviceName, methodName, _ := strings.Cut(symbol, ".")
	service := reflect.ValueOf(client).Elem().FieldByName(serviceName)
	if !service.IsValid() {
		t.Fatalf("%s: no Client field %s", symbol, serviceName)
	}
	method := service.MethodByName(methodName)
	if !method.IsValid() {
		t.Fatalf("%s: no method %s on %s", symbol, methodName, service.Type())
	}

	// Pollers and waits would otherwise run until their own deadlines.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	args := make([]reflect.Value, method.Type().NumIn())
	for i := range args {
		in := method.Type().In(i)
		switch override, ok := wireArgOverrides[symbol][i]; {
		case ok:
			args[i] = reflect.ValueOf(override).Convert(in)
		case in == reflect.TypeOf((*context.Context)(nil)).Elem():
			args[i] = reflect.ValueOf(ctx)
		default:
			args[i] = synthesize(in, 0)
		}
	}
	func() {
		defer func() { _ = recover() }() // a synthesised argument may trip a nil path; what was sent still counts
		method.Call(args)
	}()

	mu.Lock()
	defer mu.Unlock()
	return append([]wireRequest(nil), sent...)
}

// synthesize builds a non-zero value of type t, so argument validation lets
// as many methods as possible reach the wire.
func synthesize(t reflect.Type, depth int) reflect.Value {
	value := reflect.New(t).Elem()
	if depth > 4 {
		return value
	}
	switch t {
	case reflect.TypeOf(time.Duration(0)):
		return reflect.ValueOf(10 * time.Millisecond).Convert(t)
	case reflect.TypeOf(time.Time{}):
		return reflect.ValueOf(time.Unix(1790000000, 0))
	}
	switch t.Kind() {
	case reflect.String:
		value.SetString("v1")
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(1)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(1)
	case reflect.Pointer:
		value.Set(reflect.New(t.Elem()))
		value.Elem().Set(synthesize(t.Elem(), depth+1))
	case reflect.Slice:
		value.Set(reflect.MakeSlice(t, 1, 1))
		value.Index(0).Set(synthesize(t.Elem(), depth+1))
	case reflect.Map:
		value.Set(reflect.MakeMap(t))
		value.SetMapIndex(synthesize(t.Key(), depth+1), synthesize(t.Elem(), depth+1))
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).IsExported() {
				value.Field(i).Set(synthesize(t.Field(i).Type, depth+1))
			}
		}
	case reflect.Interface:
		switch {
		case reflect.TypeOf(&bytes.Buffer{}).Implements(t):
			value.Set(reflect.ValueOf(bytes.NewBufferString("v1")))
		case reflect.TypeOf(map[string]any{}).Implements(t):
			value.Set(reflect.ValueOf(map[string]any{"v1": "v1"}))
		}
	}
	return value
}

// routeDeclared reports whether method+path matches one of the declared
// operations, where each {param} in a route matches exactly one non-empty
// path segment.
func routeDeclared(declared []CoverageOperation, method, path string) bool {
	for _, operation := range declared {
		if operation.Method == method && routeMatches(operation.Route, path) {
			return true
		}
	}
	return false
}

func routeMatches(route, path string) bool {
	want := strings.Split(strings.Trim(route, "/"), "/")
	got := strings.Split(strings.Trim(path, "/"), "/")
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if strings.HasPrefix(want[i], "{") && strings.HasSuffix(want[i], "}") {
			if got[i] == "" {
				return false
			}
			continue
		}
		if want[i] != got[i] {
			return false
		}
	}
	return true
}
