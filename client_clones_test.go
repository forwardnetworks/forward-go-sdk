package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests hold the clone and fail-closed behaviour of the client across every manifest symbol, not one example each, because
// the hexagonal split (the v0.9 hexagonal split) rebuilds how every service is wired: a ForNetwork clone, a NewUnavailable client and the
// shared rate-limit cooldown must keep working for each family, including the ports a composite is given. They use only the public
// surface (NewClient, ForNetwork, NewUnavailable, the Client fields by name), so the move must pass them unchanged.

// manifestSymbols lists every manifest symbol, sorted.
func manifestSymbols() []string {
	symbols := make([]string, 0, len(sdkCoverageCatalog))
	for symbol := range sdkCoverageCatalog {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}

// callSymbol calls Client.<Service>.<Method> with synthesised arguments. With emptyFirstString, the first string argument (a
// network ID wherever a method takes one) is "", so a network-scoped method resolves the client's bound network. It returns the
// results and the value of any panic, which a caller must treat as a failure: a panic here is a nil port or a nil service.
func callSymbol(t *testing.T, client *Client, symbol string, ctx context.Context, emptyFirstString bool) (results []reflect.Value, panicked any) {
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
	args := make([]reflect.Value, method.Type().NumIn())
	blanked := false
	for i := range args {
		in := method.Type().In(i)
		switch override, ok := wireArgOverrides[symbol][i]; {
		case ok:
			args[i] = reflect.ValueOf(wireArgValue(override)).Convert(in)
		case in == reflect.TypeOf((*context.Context)(nil)).Elem():
			args[i] = reflect.ValueOf(ctx)
		case emptyFirstString && !blanked && in.Kind() == reflect.String:
			args[i] = reflect.Zero(in)
			blanked = true
		default:
			args[i] = synthesize(in, 0)
		}
	}
	func() {
		defer func() { panicked = recover() }()
		if method.Type().IsVariadic() {
			results = method.CallSlice(args)
		} else {
			results = method.Call(args)
		}
	}()
	return results, panicked
}

// lastError returns the error a method returned in its last result, if that result is an error.
func lastError(results []reflect.Value) (error, bool) {
	if len(results) == 0 {
		return nil, false
	}
	last := results[len(results)-1]
	if last.Type() != reflect.TypeOf((*error)(nil)).Elem() {
		return nil, false
	}
	if last.IsNil() {
		return nil, true
	}
	return last.Interface().(error), true
}

type requestLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *requestLog) add(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, r.Method+" "+r.URL.RequestURI())
}

func (l *requestLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// A ForNetwork clone answers for its own network: every symbol called on it with an empty network argument must never send the
// parent's network, and enough of them must send the clone's that the check is not vacuous. A port the split forgets to rebind
// to the clone resolves an empty network against the parent, which is what this catches.
func TestForNetworkCloneNeverSendsTheParentsNetwork(t *testing.T) {
	const minBound = 338
	var bound atomic.Int64
	for _, symbol := range manifestSymbols() {
		symbol := symbol
		t.Run(symbol, func(t *testing.T) {
			t.Parallel()
			for _, mode := range []domain.AuthMode{"", domain.AuthModeService, domain.AuthModeBrowser, domain.AuthModeNone} {
				log := &requestLog{}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					log.add(r)
					_, _ = io.Copy(io.Discard, r.Body)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{}`))
				}))
				parent := cloneTestClient(t, server.URL, mode, "parentnet")
				clone := parent.ForNetwork("boundnet")
				// Wide, because a missed deadline drops a request and the floor on bound-network senders would fall under -race load.
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_, panicked := callSymbol(t, clone, symbol, ctx, true)
				cancel()
				server.Close()
				if panicked != nil {
					t.Fatalf("%s panicked on a ForNetwork clone (mode %q): %v", symbol, mode, panicked)
				}
				sent := log.all()
				for _, line := range sent {
					if strings.Contains(line, "parentnet") {
						t.Errorf("%s on a clone bound to boundnet sent the parent's network (mode %q): %s", symbol, mode, line)
					}
				}
				for _, line := range sent {
					if strings.Contains(line, "boundnet") {
						bound.Add(1)
						return
					}
				}
				if len(sent) != 0 {
					return
				}
			}
		})
	}
	t.Cleanup(func() {
		t.Logf("%d symbols sent the clone's bound network", bound.Load())
		if n := bound.Load(); n < minBound {
			t.Errorf("only %d symbols sent the clone's bound network; want at least %d, or the check proves little", n, minBound)
		}
	})
}

// A client from NewUnavailable fails closed on every symbol: it never panics (a nil port or service would), and a method that
// returns an error returns one. Where the method got past its own argument checks, that error is the *UnavailableError carrying
// the cause, so a caller can tell "no credential" from "Forward refused".
func TestUnavailableClientFailsClosedOnEverySymbol(t *testing.T) {
	const minUnavailable = 787
	cause := errors.New("credential lookup failed")
	var typed atomic.Int64
	for _, symbol := range manifestSymbols() {
		symbol := symbol
		t.Run(symbol, func(t *testing.T) {
			t.Parallel()
			client := NewUnavailable("tenant-9", cause)
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			results, panicked := callSymbol(t, client, symbol, ctx, false)
			if panicked != nil {
				t.Fatalf("%s panicked on an unavailable client: %v", symbol, panicked)
			}
			err, returnsError := lastError(results)
			if !returnsError {
				return
			}
			if reason, listed := unavailableMaySucceed[symbol]; listed {
				if err != nil {
					t.Errorf("%s is listed as succeeding on an unavailable client (%s) but returned %v; take it off the list", symbol, reason, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s succeeded on an unavailable client", symbol)
			}
			var unavailable *domain.UnavailableError
			if errors.As(err, &unavailable) {
				if !errors.Is(err, cause) || unavailable.NetworkID != "tenant-9" {
					t.Errorf("%s: unavailable error lost its cause or network: %v", symbol, err)
				}
				typed.Add(1)
			}
		})
	}
	t.Cleanup(func() {
		t.Logf("%d symbols returned *UnavailableError", typed.Load())
		if n := typed.Load(); n < minUnavailable {
			t.Errorf("only %d symbols returned *UnavailableError; want at least %d", n, minUnavailable)
		}
	})
}

// The rate limiter's lockout is one per credential, so it is one per client and every clone: once the parent has seen it, no
// symbol of any family on a ForNetwork clone may send into it. Each call gets a context shorter than the lockout, so a symbol
// that shares the cooldown waits and sends nothing; one wired to a cooldown of its own reaches the server.
func TestForNetworkCloneSharesTheCooldownWithEveryFamily(t *testing.T) {
	const minHeld = 765
	var held atomic.Int64
	locked := atomic.Bool{}
	var leaks requestLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if !locked.Load() {
			locked.Store(true)
			w.Header().Set("Retry-After", "30")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"message":"Rate limit exceeded, please try again later"}`)
			return
		}
		leaks.add(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	parent, err := NewClient(Config{BaseURL: server.URL, Username: "user", Password: "pass", NetworkID: "parentnet",
		HTTPClient: privateHTTPClient(), Retry: RetryPolicy{MaxAttempts: 2, Delay: time.Millisecond, MaxDelay: time.Minute}})
	if err != nil {
		t.Fatal(err)
	}
	// Trigger the lockout from the parent; the call itself waits out its own short context.
	trigger, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_, _, _ = parent.Networks.List(trigger)
	cancel()
	if !locked.Load() {
		t.Fatal("the lockout was never served, so the test checks nothing")
	}
	clone := parent.ForNetwork("boundnet")

	t.Run("symbols", func(t *testing.T) {
		for _, symbol := range manifestSymbols() {
			symbol := symbol
			if _, silent := wireGoldenSilent[symbol]; silent {
				continue
			}
			t.Run(symbol, func(t *testing.T) {
				t.Parallel()
				before := len(leaks.all())
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				_, panicked := callSymbol(t, clone, symbol, ctx, false)
				if panicked != nil {
					t.Fatalf("%s panicked on a ForNetwork clone: %v", symbol, panicked)
				}
				_ = before
				held.Add(1)
			})
		}
	})
	if sent := leaks.all(); len(sent) != 0 {
		shown := sent
		if len(shown) > 10 {
			shown = shown[:10]
		}
		t.Errorf("%d requests were sent into the parent's lockout from the clone; the first: %s", len(sent), strings.Join(shown, "; "))
	}
	t.Logf("%d symbols called on the clone during the lockout", held.Load())
	if n := held.Load(); n < minHeld {
		t.Errorf("only %d symbols were held by the shared cooldown; want at least %d", n, minHeld)
	}
}

func cloneTestClient(t *testing.T, baseURL string, mode domain.AuthMode, network string) *Client {
	t.Helper()
	cfg := Config{BaseURL: baseURL, Username: "user", Password: "pass", NetworkID: network, AuthMode: mode, HTTPClient: privateHTTPClient()}
	switch mode {
	case domain.AuthModeBrowser:
		cfg.Username, cfg.Password = "", ""
		cfg.Cookies = []*http.Cookie{{Name: "SESSION", Value: "v1"}, {Name: "XSRF-TOKEN", Value: "v1"}}
	case domain.AuthModeNone:
		cfg.Username, cfg.Password = "", ""
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient(%q): %v", mode, err)
	}
	return client
}

// unavailableMaySucceed names the symbols that return no error on an unavailable client, each for a reason that sends nothing and
// claims nothing. A symbol listed here that starts failing must come off.
var unavailableMaySucceed = map[string]string{}

var _ = fmt.Sprintf
