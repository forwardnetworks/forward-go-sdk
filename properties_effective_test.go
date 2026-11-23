package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A caller must be able to tell where an org's value comes from: an org row beats a global override, which beats the default. The
// per-property overrides list cannot say so (it also holds orphan rows of deleted orgs), so the source is read from the org's and the
// global CONFIGURED views, and the value from the org's own read. Keys come back lower-case on 26.11.
func TestEffectiveOrganizationValueSource(t *testing.T) {
	cases := []struct {
		name, orgConfigured, globalConfigured string
		want                                  domain.PropertySource
	}{
		{"org row wins over global", `{"parser":"PROD"}`, `{"parser":"DEV"}`, domain.PropertySourceOrganization},
		{"global override", `{"other":1}`, `{"PARSER":"DEV"}`, domain.PropertySourceGlobal},
		{"built-in default", `{}`, `{"other":1}`, domain.PropertySourceDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.EscapedPath() + "?" + r.URL.RawQuery {
				case "/api/orgs/102/config/PARSER?":
					_, _ = io.WriteString(w, `{"parser":"DEV"}`)
				case "/api/orgs/102/config?filter=CONFIGURED":
					_, _ = io.WriteString(w, tc.orgConfigured)
				case "/api/global-config?filter=CONFIGURED":
					_, _ = io.WriteString(w, tc.globalConfigured)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			c := newTestClient(t, server.URL)
			got, _, err := c.Properties.EffectiveOrganizationValue(context.Background(), "102", "PARSER")
			if err != nil || got.Source != tc.want || string(got.Value) != `"DEV"` {
				t.Fatalf("got %+v err=%v, want source %s and value DEV", got, err, tc.want)
			}
		})
	}
}
