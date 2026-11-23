package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// VulnerabilitiesService reads Forward's vulnerability analysis: which CVEs
// may affect the network's devices, and Forward's per-device verdict. The
// three reads are the published Vulnerability Analysis API
// (fwd/api/apis/vulnerability-analysis.yaml, VulnerabilityAnalysisController),
// served on primary 15398425a69 and stable 67e89c87124.
//
// NQE's device.cveFindings carries only a boolean isVulnerable; the detection
// result here (VULNERABLE, OS_VULNERABLE, UNCONFIRMED, ...) says how sure
// Forward is, which is what an investigation has to report.
type Vulnerabilities service

// List returns the CVEs that may affect the network, each with per-OS device
// counts by detection result. GET /api/networks/{networkId}/vulnerabilities?v=2
// (getVulnerabilities). For per-device verdicts on one CVE, use Get.
func (s *Vulnerabilities) List(ctx context.Context, networkID string, options ports.VulnerabilityListOptions) (*ports.VulnerabilityAnalysis, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"v": []string{"2"}}
	setString(query, "snapshotId", options.SnapshotID)
	if options.InternetAddressable != nil {
		query.Set("internetAddressable", strconv.FormatBool(*options.InternetAddressable))
	}
	addNonEmpty(query, "location", options.LocationIDs)
	addNonEmpty(query, "tag", options.Tags)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/vulnerabilities?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Vulnerabilities.List")
	out := new(ports.VulnerabilityAnalysis)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// Get returns one CVE with Forward's verdict for every possibly affected
// device, and the configuration lines behind each. GET
// /api/networks/{networkId}/vulnerabilities/{cveId} (getVulnerability). An
// empty snapshotID uses the latest processed snapshot.
func (s *Vulnerabilities) Get(ctx context.Context, networkID, cveID, snapshotID string) (*ports.VulnerabilityWithDevices, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	if cveID = strings.TrimSpace(cveID); cveID == "" {
		return nil, nil, errors.New("forward: CVE ID is required")
	}
	path += "/vulnerabilities/" + url.PathEscape(cveID)
	query := url.Values{}
	setString(query, "snapshotId", snapshotID)
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Vulnerabilities.Get")
	out := new(ports.VulnerabilityWithDevices)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// ListByOS returns the full analysis as one row per CVE-OS combination, with
// a result per possibly affected device. GET
// /api/networks/{networkId}/vulnerabilities (getOsVulnerabilities, the v1
// form). The response can be large; page it with Offset and Limit.
func (s *Vulnerabilities) ListByOS(ctx context.Context, networkID string, options ports.VulnerabilityByOSOptions) (*ports.OSVulnerabilityAnalysis, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	path += "/vulnerabilities"
	query := url.Values{}
	setString(query, "snapshotId", options.SnapshotID)
	setInt32(query, "offset", options.Offset)
	setInt32(query, "limit", options.Limit)
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Vulnerabilities.ListByOS")
	out := new(ports.OSVulnerabilityAnalysis)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

func addNonEmpty(query url.Values, key string, values []string) {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			query.Add(key, value)
		}
	}
}

// ListDevices returns the network's devices with the CVEs that may affect them,
// counted by detection result, severity, age and exploit, in one call -- the
// inverse of List and Get, which are per CVE. A device with no CVE matching the
// filters is left out. GET /api/networks/{networkId}/device-vulnerabilities
// (VulnerabilityAnalysisController.getDeviceVulnerabilities, served on primary
// 15398425a69 and stable 67e89c87124; VIEW_SECURITY_ANALYSIS). An empty
// SnapshotID uses the latest processed snapshot. Preview: not in the published
// spec.
func (s *Vulnerabilities) ListDevices(ctx context.Context, networkID string, options ports.DeviceVulnerabilityListOptions) (*ports.DeviceVulnerabilities, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	switch options.Severity {
	case "", ports.VulnerabilitySeverityNone, ports.VulnerabilitySeverityLow, ports.VulnerabilitySeverityMedium, ports.VulnerabilitySeverityHigh, ports.VulnerabilitySeverityCritical:
	default:
		return nil, nil, fmt.Errorf("forward: invalid CVE severity %q", options.Severity)
	}
	switch options.Age {
	case "", ports.CVEAgeMonth, ports.CVEAgeYear, ports.CVEAgeOlder:
	default:
		return nil, nil, fmt.Errorf("forward: invalid CVE age %q", options.Age)
	}
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{}
	setString(query, "snapshotId", options.SnapshotID)
	setString(query, "severity", string(options.Severity))
	setString(query, "age", string(options.Age))
	if options.Exploit != nil {
		query.Set("exploit", strconv.FormatBool(*options.Exploit))
	}
	path += "/device-vulnerabilities"
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Vulnerabilities.ListDevices")
	out := new(ports.DeviceVulnerabilities)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

var cveIDPattern = regexp.MustCompile(`^(?i:cve)-[0-9]{4}-[0-9]{4,}$`)

// DetectionLogic reads a CVE's configuration detection logic (GET /api/cves/{cveId}/detection-logic;
// VulnerabilityAnalysisController.getCveConfigDetectionLogic at release/26.11.0-10 and master). The route is ORG-scoped: it takes no network
// and no snapshot and is not under /api/networks, so a guard keyed on the network sees only the path.
//
// It needs the organization property CVE_DETECTION_LOGIC_EXPOSED (a GUI property, off by default, on-premises configurable, "enabled for
// select strategic customers"); with it off Forward answers a 403 whose message is "CVE_DETECTION_LOGIC_EXPOSED is off for your
// organization", which classifies as ErrFeatureGated (ErrorResponse.GateProperty names it). A CVE the index has never heard of is a 404; a
// CVE the index has whose impact is not configuration-dependent answers 200 with an empty SourceType and Source. cveID must look like
// CVE-YYYY-NNNN (a four-digit year and at least four digits, as Forward's CveId requires); lower case is accepted and sent upper-cased.
// Preview, from Forward's source and controller test, not a live call.
func (s *Vulnerabilities) DetectionLogic(ctx context.Context, cveID string) (*ports.CVEDetectionLogic, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	cveID = strings.TrimSpace(cveID)
	if !cveIDPattern.MatchString(cveID) {
		return nil, nil, fmt.Errorf("forward: %q is not a CVE id (CVE-YYYY-NNNN)", cveID)
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/cves/"+url.PathEscape(strings.ToUpper(cveID))+"/detection-logic", nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.CVEDetectionLogic)
	resp, err := s.client.Do(markOperation(req, "Vulnerabilities.DetectionLogic"), out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
