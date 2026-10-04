package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestBrowserLoginUsesOnlyLiveRoutes: Forward's browser login is
// GET /api/public/csrf (PublicController, under the /api servlet) followed by
// the Spring Security form login at POST /login (SecurityConfig
// .loginProcessingUrl(PagePaths.LOGIN)). Neither POST /api/auth/login nor a
// root GET /public/csrf exists on the primary 15398425a69 or stable
// 67e89c87124 builds, so Login must not spend a request on either.
func TestBrowserLoginUsesOnlyLiveRoutes(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /api/public/csrf":
			http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "prelogin", Path: "/"})
			_, _ = io.WriteString(w, `{"headerName":"X-CSRF-TOKEN","parameterName":"_csrf","token":"t-1"}`)
		case "POST /login":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.PostForm.Get("_csrf") != "t-1" || r.PostForm.Get("username") != "alice" || r.PostForm.Get("password") != "pw" {
				t.Errorf("login form = %v", r.PostForm)
			}
			http.SetCookie(w, &http.Cookie{Name: "SESSION", Value: "s-1", Path: "/"})
			_, _ = io.WriteString(w, `{"location":"/"}`)
		default:
			http.Error(w, "No endpoint "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "alice", Password: "pw", AuthMode: AuthModeBrowser})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Browser.Login(context.Background())
	if err != nil {
		t.Fatalf("Login: %v (calls %v)", err, calls)
	}
	found := false
	for _, cookie := range result.Cookies {
		found = found || (cookie.Name == "SESSION" && cookie.Value == "s-1")
	}
	if !found {
		t.Fatalf("login cookies = %#v", result.Cookies)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"GET /api/public/csrf", "POST /login"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

// TestBrowserLoginFallsBackToLoginPageCSRF: when the public CSRF endpoint
// fails, the token is read off the login page, still without touching a dead
// route.
func TestBrowserLoginFallsBackToLoginPageCSRF(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /login":
			_, _ = io.WriteString(w, `<html><script>window._csrf={"parameterName":"_csrf","token":"page-token"}</script></html>`)
		case "POST /login":
			_ = r.ParseForm()
			if r.PostForm.Get("_csrf") != "page-token" {
				t.Errorf("login form = %v", r.PostForm)
			}
			http.SetCookie(w, &http.Cookie{Name: "SESSION", Value: "s-2", Path: "/"})
			_, _ = io.WriteString(w, `{"location":"/"}`)
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "alice", Password: "pw", AuthMode: AuthModeBrowser})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Browser.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v (calls %v)", err, calls)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, call := range calls {
		if call == "POST /api/auth/login" || call == "GET /public/csrf" {
			t.Fatalf("Login called dead route %s (calls %v)", call, calls)
		}
	}
}
