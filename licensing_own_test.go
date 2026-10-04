package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// GET /api/licenses works on every deployment (the org-scoped
// /api/orgs/{id}/licenses is SaaS-only and 404s "No endpoint" on-prem).
func TestLicensingOwnOrg(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/api/licenses":
			_, _ = io.WriteString(w, `[{"id":"L1","license":{"id":"L1","tier":"ENTERPRISE","expiresAt":"2027-01-01T00:00:00Z"},"status":"ACTIVE"}]`)
		case "/api/licenses?view=status":
			_, _ = io.WriteString(w, `{"licenseTier":"ENTERPRISE","licenseStatus":"ACTIVE"}`)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	licensing := newTestClient(t, server.URL).Licensing
	ctx := context.Background()

	list, _, err := licensing.List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "L1" || list[0].Status != "ACTIVE" || list[0].License == nil || list[0].License.Tier != "ENTERPRISE" {
		t.Fatalf("List() = %+v, %v", list, err)
	}
	tier, _, err := licensing.TierAndStatus(ctx)
	if err != nil || tier.LicenseTier != "ENTERPRISE" || tier.LicenseStatus != "ACTIVE" {
		t.Fatalf("TierAndStatus() = %+v, %v", tier, err)
	}
}
