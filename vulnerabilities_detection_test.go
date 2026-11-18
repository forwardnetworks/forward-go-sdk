package forward

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The contract (VulnerabilityAnalysisController.getCveConfigDetectionLogic and its controller test): the route is org-scoped, the answer is
// {cveId, sourceType, source} with both of the last two null for a CVE that has no detection logic, an unknown CVE is a 404, and the gate
// property off is a 403 that classifies as a feature gate.
func TestVulnerabilitiesDetectionLogic(t *testing.T) {
	var path string
	calls := 0
	status, body := http.StatusOK, `{"cveId":"CVE-2011-0946","sourceType":"CONFIG_BLOCK_PATTERN","source":"ip http server\n  no ip http secure-server"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.RequestURI()
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	logic, _, err := client.Vulnerabilities.DetectionLogic(ctx, " cve-2011-0946 ")
	if err != nil || !logic.HasLogic() || logic.SourceType != CVEDetectionConfigBlockPattern || logic.Source != "ip http server\n  no ip http secure-server" || logic.CVEID != "CVE-2011-0946" {
		t.Fatalf("logic=%+v err=%v", logic, err)
	}
	if path != "/api/cves/CVE-2011-0946/detection-logic" {
		t.Fatalf("path = %s: no network or snapshot, the id upper-cased", path)
	}

	body = `{"cveId":"CVE-2021-1234","sourceType":null,"source":null}`
	none, _, err := client.Vulnerabilities.DetectionLogic(ctx, "CVE-2021-1234")
	if err != nil || none.HasLogic() || none.SourceType != "" || none.Source != "" {
		t.Fatalf("a CVE without detection logic is a 200 with empty fields: %+v %v", none, err)
	}

	status, body = http.StatusForbidden, `{"message":"CVE_DETECTION_LOGIC_EXPOSED is off for your organization"}`
	_, _, err = client.Vulnerabilities.DetectionLogic(ctx, "CVE-2021-1234")
	var apiErr *ErrorResponse
	if !errors.Is(err, ErrFeatureGated) || !errors.As(err, &apiErr) || apiErr.GateProperty != "CVE_DETECTION_LOGIC_EXPOSED" || apiErr.GateEnabled {
		t.Fatalf("the gate off must be a feature gate naming the property: %v", err)
	}

	status, body = http.StatusNotFound, `{"message":"CVE-2099-0001 not found"}`
	if _, _, err = client.Vulnerabilities.DetectionLogic(ctx, "CVE-2099-0001"); !IsStatus(err, http.StatusNotFound) || errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("an unknown CVE is a plain 404: %v", err)
	}

	for _, bad := range []string{"", "CVE_2021_1234", "CVE-2021-123", "CVE-202-1234", "CVE-2o21-1234", "CVE-2021-1234/../x", "../CVE-2021-1234"} {
		before := calls
		if _, _, err := client.Vulnerabilities.DetectionLogic(ctx, bad); err == nil || calls != before {
			t.Errorf("%q must be refused before any request is made (err %v, requests %d)", bad, err, calls-before)
		}
	}
}
