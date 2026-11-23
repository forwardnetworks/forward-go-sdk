package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"context"
	"fmt"
	"net/http"

	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeForwardSession models the parts of Forward's session chain the service
// identity relies on: CSRF on writes bound to the session, a token that changes
// when the principal does, SP-initiated SAML with a POST-binding form, and the
// ACS that authenticates the session.
type fakeForwardSession struct {
	mu         sync.Mutex
	principal  map[string]string // session cookie -> principal
	token      map[string]string // session cookie -> CSRF token
	csrfFetch  int
	writes     []string
	acsBodies  []url.Values
	nextSessID int
}

func (f *fakeForwardSession) session(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie("SESSION"); err == nil {
		if _, ok := f.principal[c.Value]; ok {
			return c.Value
		}
	}
	f.nextSessID++
	id := fmt.Sprintf("s%d", f.nextSessID)
	f.principal[id] = ""
	f.token[id] = fmt.Sprintf("t-%s-anon", id)
	http.SetCookie(w, &http.Cookie{Name: "SESSION", Value: id, Path: "/"})
	return id
}

func (f *fakeForwardSession) authenticate(id, principal string) {
	f.principal[id] = principal
	f.token[id] = fmt.Sprintf("t-%s-%s", id, principal)
}

func (f *fakeForwardSession) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.session(w, r)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/public/csrf":
		f.csrfFetch++
		fmt.Fprintf(w, `{"headerName":"X-CSRF-TOKEN","parameterName":"_csrf","token":%q}`, f.token[id])
	case r.Method == http.MethodPost && r.URL.Path == "/login":
		_ = r.ParseForm()
		if r.PostForm.Get("_csrf") != f.token[id] {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		f.authenticate(id, r.PostForm.Get("username"))
		http.Redirect(w, r, "/", http.StatusFound)
	case r.Method == http.MethodGet && r.URL.Path == "/api/admin/impersonate":
		if f.principal[id] != "forward" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		f.authenticate(id, "user-"+strings.TrimSuffix(r.URL.Query().Get("user"), ".full"))
		http.Redirect(w, r, "/", http.StatusFound)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/saml2/authenticate/"):
		reg := strings.TrimPrefix(r.URL.Path, "/saml2/authenticate/")
		fmt.Fprintf(w, `<html><body onload="document.forms[0].submit()"><form method="post" action="https://idp.example/application/saml/%s/">`+
			`<input type="hidden" name="SAMLRequest" value="req&#43;%s"/><input type="hidden" value="rs-1" name="RelayState"/></form></body></html>`, reg, reg)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/login/saml2/sso/"):
		_ = r.ParseForm()
		f.acsBodies = append(f.acsBodies, r.PostForm)
		if r.Header.Get("X-CSRF-TOKEN") != "" {
			w.WriteHeader(http.StatusBadRequest) // the SDK must not send a pre-auth token here
			return
		}
		if r.PostForm.Get("SAMLResponse") != "good" {
			http.Redirect(w, r, "/login?error", http.StatusFound)
			return
		}
		f.authenticate(id, "saml-svc")
		http.Redirect(w, r, "/", http.StatusFound)
	case r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/login"):
		fmt.Fprint(w, "<html></html>")
	case r.Method == http.MethodGet && r.URL.Path == "/api/users/current":
		if f.principal[id] == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(w, `{"user":{"id":"2257","username":%q,"authSource":"SAML","externalGroups":["sf-org-admins"]},"roles":{"org":["ADMIN"],"network":{}}}`, f.principal[id])
	case r.Method == http.MethodPost && r.URL.Path == "/api/networks":
		if f.principal[id] == "" || r.Header.Get("X-CSRF-TOKEN") != f.token[id] {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		f.writes = append(f.writes, f.principal[id])
		fmt.Fprint(w, `{"id":"4744","name":"probe"}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestBrowserSAMLAssertionConsumerRequiresARegistration(t *testing.T) {
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: "https://fwd.example", AuthMode: domain.AuthModeBrowser})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Browser.SAMLAssertionConsumer(context.Background(), " ", url.Values{"SAMLResponse": {"good"}}); err == nil {
		t.Fatal("an assertion without a registration id was accepted")
	}
}
