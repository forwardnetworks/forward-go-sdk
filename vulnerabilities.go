package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
type VulnerabilitiesService service

// VulnerabilitySeverity is the NVD qualitative rating of a CVE's best CVSS
// score (v4, then v3, then v2); NONE when the CVE has no score.
type VulnerabilitySeverity string

const (
	VulnerabilitySeverityNone     VulnerabilitySeverity = "NONE"
	VulnerabilitySeverityLow      VulnerabilitySeverity = "LOW"
	VulnerabilitySeverityMedium   VulnerabilitySeverity = "MEDIUM"
	VulnerabilitySeverityHigh     VulnerabilitySeverity = "HIGH"
	VulnerabilitySeverityCritical VulnerabilitySeverity = "CRITICAL"
)

// CVEDetectionResult is Forward's verdict for one device and CVE
// (DetectionResult). VULNERABLE: confirmed from the configuration.
// OS_VULNERABLE: the OS version is affected and the CVE does not depend on
// configuration. UNCONFIRMED: the OS version is affected and the CVE depends
// on configuration Forward could not confirm. UNIMPLEMENTED: the OS version is
// affected and Forward has no configuration analysis for this CVE yet.
// NOT_VULNERABLE: the configuration rules it out.
type CVEDetectionResult string

const (
	CVEDetectionVulnerable    CVEDetectionResult = "VULNERABLE"
	CVEDetectionOSVulnerable  CVEDetectionResult = "OS_VULNERABLE"
	CVEDetectionUnimplemented CVEDetectionResult = "UNIMPLEMENTED"
	CVEDetectionUnconfirmed   CVEDetectionResult = "UNCONFIRMED"
	CVEDetectionNotVulnerable CVEDetectionResult = "NOT_VULNERABLE"
)

// DeviceVulnerabilityStatus is the coarse status Forward derives from the
// detection result (and any custom analysis): VULNERABLE,
// POTENTIALLY_VULNERABLE or NOT_VULNERABLE.
type DeviceVulnerabilityStatus string

const (
	DeviceVulnerable            DeviceVulnerabilityStatus = "VULNERABLE"
	DevicePotentiallyVulnerable DeviceVulnerabilityStatus = "POTENTIALLY_VULNERABLE"
	DeviceNotVulnerable         DeviceVulnerabilityStatus = "NOT_VULNERABLE"
)

// CVEInfo is the OS-independent part of a CVE (CveInfo).
type CVEInfo struct {
	ID              string   `json:"id"`
	Description     string   `json:"description,omitempty"`
	HasCISAKEVEntry bool     `json:"hasCisaKevEntry,omitempty"`
	Weaknesses      []string `json:"weaknesses,omitempty"`
}

// CVEOSInfo is what a CVE means for one affected OS (CveOsInfo). Scores are
// nil when the CVE has none of that CVSS version. ConfigDependent is nil when
// unknown; ConfigAnalysis is UNSUPPORTED, IN_PROGRESS or SUPPORTED.
type CVEOSInfo struct {
	Vendor                  string                `json:"vendor,omitempty"`
	OS                      string                `json:"os,omitempty"`
	Severity                VulnerabilitySeverity `json:"severity,omitempty"`
	V2Score                 *float64              `json:"v2Score,omitempty"`
	V3Score                 *float64              `json:"v3Score,omitempty"`
	V4Score                 *float64              `json:"v4Score,omitempty"`
	URL                     string                `json:"url,omitempty"`
	PublishDate             string                `json:"publishDate,omitempty"`
	AdvisoryMentionsExploit bool                  `json:"advisoryMentionsExploit,omitempty"`
	ConfigDependent         *bool                 `json:"configDependent,omitempty"`
	ConfigAnalysis          string                `json:"configAnalysis,omitempty"`
}

// CVEResultCount is how many devices got one detection result.
type CVEResultCount struct {
	Result      CVEDetectionResult `json:"result"`
	DeviceCount int                `json:"deviceCount"`
}

// VulnerabilityOSSummary is a CVE on one OS with device counts
// (CveOsInfoWithDeviceCounts). ResultCounts adds up to DeviceCount.
type VulnerabilityOSSummary struct {
	CVEOSInfo
	OSVersions   []string         `json:"osVersions,omitempty"`
	DeviceCount  int              `json:"deviceCount"`
	ResultCounts []CVEResultCount `json:"resultCounts,omitempty"`
	LocationIDs  []string         `json:"locationIds,omitempty"`
	Tags         []string         `json:"tags,omitempty"`
}

// Vulnerability is one CVE that may affect the network, summarized per OS.
type Vulnerability struct {
	CVEInfo
	OSInfos []VulnerabilityOSSummary `json:"osInfos"`
}

// VulnerabilityAnalysis is the network's CVE list plus the provenance of the
// CVE index it was computed from.
type VulnerabilityAnalysis struct {
	Vulnerabilities   []Vulnerability `json:"vulnerabilities"`
	IndexCreatedAt    string          `json:"indexCreatedAt,omitempty"`
	IndexUploadedAt   string          `json:"indexUploadedAt,omitempty"`
	IndexUploadedBy   string          `json:"indexUploadedBy,omitempty"`
	IndexUploadedByID string          `json:"indexUploadedById,omitempty"`
}

// VulnerableDevice is one possibly affected device with Forward's verdict
// (VulnerabilityDeviceWithResult). FileRanges maps a device file name to the
// configuration lines behind the verdict; read them with Devices.DownloadFile.
// InternetAddressable is nil when Forward did not say.
type VulnerableDevice struct {
	Name                string                    `json:"name"`
	OSVersion           string                    `json:"osVersion,omitempty"`
	Model               string                    `json:"model,omitempty"`
	ManagementIPs       []string                  `json:"managementIps,omitempty"`
	Tags                []string                  `json:"tags,omitempty"`
	LocationID          string                    `json:"locationId,omitempty"`
	InternetAddressable *bool                     `json:"internetAddressable,omitempty"`
	Status              DeviceVulnerabilityStatus `json:"status,omitempty"`
	Result              CVEDetectionResult        `json:"result,omitempty"`
	FileRanges          map[string][]LineRange    `json:"fileRanges,omitempty"`
}

// VulnerabilityOSDevices is a CVE on one OS with every affected device
// (CveOsInfoWithDevices). Description is the advisory text published at URL.
type VulnerabilityOSDevices struct {
	CVEOSInfo
	Description string             `json:"description,omitempty"`
	Devices     []VulnerableDevice `json:"devices,omitempty"`
}

// VulnerabilityWithDevices is one CVE with a verdict per affected device.
type VulnerabilityWithDevices struct {
	CVEInfo
	OSInfos []VulnerabilityOSDevices `json:"osInfos"`
}

// VulnerabilityListOptions scopes List. An empty SnapshotID uses the latest
// processed snapshot; LocationIDs and Tags narrow the devices analyzed (a
// device needs at least one of the tags).
type VulnerabilityListOptions struct {
	SnapshotID          string
	InternetAddressable *bool
	LocationIDs         []string
	Tags                []string
}

// List returns the CVEs that may affect the network, each with per-OS device
// counts by detection result. GET /api/networks/{networkId}/vulnerabilities?v=2
// (getVulnerabilities). For per-device verdicts on one CVE, use Get.
func (s *VulnerabilitiesService) List(ctx context.Context, networkID string, options VulnerabilityListOptions) (*VulnerabilityAnalysis, *Response, error) {
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
	out := new(VulnerabilityAnalysis)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// Get returns one CVE with Forward's verdict for every possibly affected
// device, and the configuration lines behind each. GET
// /api/networks/{networkId}/vulnerabilities/{cveId} (getVulnerability). An
// empty snapshotID uses the latest processed snapshot.
func (s *VulnerabilitiesService) Get(ctx context.Context, networkID, cveID, snapshotID string) (*VulnerabilityWithDevices, *Response, error) {
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
	out := new(VulnerabilityWithDevices)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// OSVulnerabilityDeviceResult is one device's configuration analysis for a
// CVE (VulnerabilityDetectionResult). Vulnerable is nil when an error stopped
// the analysis.
type OSVulnerabilityDeviceResult struct {
	Device     string                 `json:"device"`
	Vulnerable *bool                  `json:"vulnerable,omitempty"`
	FileLines  map[string][]LineRange `json:"fileLines,omitempty"`
}

// OSVulnerability is one CVE-OS combination (OsVulnerability). DetectionMethod
// is OS_VERSION (Devices lists the possibly affected devices) or CONFIG
// (DeviceResults has a configuration verdict per device). KnownExploitSource
// is CISA, VENDOR or empty.
type OSVulnerability struct {
	ID                 string                        `json:"id"`
	Description        string                        `json:"description,omitempty"`
	Severity           VulnerabilitySeverity         `json:"severity,omitempty"`
	URL                string                        `json:"url,omitempty"`
	PublishedDate      string                        `json:"publishedDate,omitempty"`
	V2Score            *float64                      `json:"v2Score,omitempty"`
	V3Score            *float64                      `json:"v3Score,omitempty"`
	V4Score            *float64                      `json:"v4Score,omitempty"`
	KnownExploitSource string                        `json:"knownExploitSource,omitempty"`
	Weaknesses         []string                      `json:"weaknesses,omitempty"`
	Vendor             string                        `json:"vendor,omitempty"`
	OS                 string                        `json:"os,omitempty"`
	OSVersions         []string                      `json:"osVersions,omitempty"`
	DependsOnConfig    *bool                         `json:"dependsOnConfig,omitempty"`
	DetectionMethod    string                        `json:"detectionMethod,omitempty"`
	Devices            []string                      `json:"devices,omitempty"`
	DeviceResults      []OSVulnerabilityDeviceResult `json:"deviceResults,omitempty"`
}

// OSVulnerabilityAnalysis is one page of the CVE-OS view. Total counts every
// row; Offset is how many were skipped before this page.
type OSVulnerabilityAnalysis struct {
	Vulnerabilities []OSVulnerability `json:"vulnerabilities"`
	Offset          int               `json:"offset"`
	Total           int               `json:"total"`
	IndexCreatedAt  string            `json:"indexCreatedAt,omitempty"`
	IndexUploadedAt string            `json:"indexUploadedAt,omitempty"`
	IndexUploadedBy string            `json:"indexUploadedBy,omitempty"`
}

// VulnerabilityByOSOptions pages ListByOS. Forward defaults Offset to 0 and
// Limit to 1000.
type VulnerabilityByOSOptions struct {
	SnapshotID string
	Offset     *int32
	Limit      *int32
}

// ListByOS returns the full analysis as one row per CVE-OS combination, with
// a result per possibly affected device. GET
// /api/networks/{networkId}/vulnerabilities (getOsVulnerabilities, the v1
// form). The response can be large; page it with Offset and Limit.
func (s *VulnerabilitiesService) ListByOS(ctx context.Context, networkID string, options VulnerabilityByOSOptions) (*OSVulnerabilityAnalysis, *Response, error) {
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
	out := new(OSVulnerabilityAnalysis)
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

// CVEAge is how long ago a CVE was published, bucketed by Forward (CveAge):
// MONTH is up to 30 days, YEAR up to 365, OLDER anything else, including a CVE
// with no published date.
type CVEAge string

const (
	CVEAgeMonth CVEAge = "MONTH"
	CVEAgeYear  CVEAge = "YEAR"
	CVEAgeOlder CVEAge = "OLDER"
)

// DeviceCVECounts is one device with how many CVEs may affect it, counted four
// ways (DeviceWithCveCounts). Each map's counts sum to the same total, and a
// bucket with no CVEs is absent. HasExploitToCVECount is keyed "true" and
// "false". InternetAddressable is nil when Forward has not computed internet
// exposure for the snapshot, which is different from false. Summary is
// VULNERABLE, POTENTIALLY_VULNERABLE or NOT_VULNERABLE; the custom counts are
// present only when the org has custom CVE analysis.
type DeviceCVECounts struct {
	Name                 string                        `json:"name"`
	OSVersion            string                        `json:"osVersion,omitempty"`
	Model                string                        `json:"model,omitempty"`
	ManagementIPs        []string                      `json:"managementIps,omitempty"`
	Tags                 []string                      `json:"tags,omitempty"`
	LocationID           string                        `json:"locationId,omitempty"`
	ResultToCVECount     map[CVEDetectionResult]int    `json:"resultToCveCount"`
	SeverityToCVECount   map[VulnerabilitySeverity]int `json:"severityToCveCount"`
	AgeToCVECount        map[CVEAge]int                `json:"ageToCveCount"`
	HasExploitToCVECount map[string]int                `json:"hasExploitToCveCount"`
	InternetAddressable  *bool                         `json:"internetAddressable,omitempty"`
	CustomLabelCounts    []CustomLabelDeviceCount      `json:"customLabelCounts,omitempty"`
	CustomStatusCounts   []CustomStatusDeviceCount     `json:"customStatusCounts,omitempty"`
	Summary              DeviceVulnerabilityStatus     `json:"summary"`
}

// CustomLabelDeviceCount is how many of one device's CVEs carry a custom label.
// (Forward names the field deviceCount, but on this route the thing counted
// is CVEs.)
type CustomLabelDeviceCount struct {
	Label       string `json:"label"`
	DeviceCount int    `json:"deviceCount"`
}

// CustomStatusDeviceCount is how many of one device's CVEs have a custom status
// (VULNERABLE or NOT_VULNERABLE). Forward names the field deviceCount, but on
// this route the thing counted is CVEs.
type CustomStatusDeviceCount struct {
	Status      string `json:"status"`
	DeviceCount int    `json:"deviceCount"`
}

// CVEsWithExploit returns how many of the device's CVEs have a known exploit.
func (d DeviceCVECounts) CVEsWithExploit() int { return d.HasExploitToCVECount["true"] }

// TotalCVEs returns how many CVEs may affect the device.
func (d DeviceCVECounts) TotalCVEs() int {
	total := 0
	for _, count := range d.ResultToCVECount {
		total += count
	}
	return total
}

// DeviceVulnerabilities is every device with a possible CVE, each with its
// counts. TotalDevices counts the devices across ALL CVEs, so it can exceed
// len(Devices) when a filter leaves some devices with no matching CVE.
// IndexCreatedAt is when the CVE index behind the result was built.
type DeviceVulnerabilities struct {
	Devices        []DeviceCVECounts `json:"devices"`
	TotalDevices   int               `json:"totalDevices"`
	IndexCreatedAt string            `json:"indexCreatedAt,omitempty"`
}

// DeviceVulnerabilityListOptions narrows ListDevices to the CVEs that count
// toward each device: all empty means every CVE. There is no internet
// filter here; read each device's InternetAddressable.
type DeviceVulnerabilityListOptions struct {
	SnapshotID string
	Severity   VulnerabilitySeverity
	Age        CVEAge
	Exploit    *bool
}

// ListDevices returns the network's devices with the CVEs that may affect them,
// counted by detection result, severity, age and exploit, in one call -- the
// inverse of List and Get, which are per CVE. A device with no CVE matching the
// filters is left out. GET /api/networks/{networkId}/device-vulnerabilities
// (VulnerabilityAnalysisController.getDeviceVulnerabilities, served on primary
// 15398425a69 and stable 67e89c87124; VIEW_SECURITY_ANALYSIS). An empty
// SnapshotID uses the latest processed snapshot. Preview: not in the published
// spec.
func (s *VulnerabilitiesService) ListDevices(ctx context.Context, networkID string, options DeviceVulnerabilityListOptions) (*DeviceVulnerabilities, *Response, error) {
	switch options.Severity {
	case "", VulnerabilitySeverityNone, VulnerabilitySeverityLow, VulnerabilitySeverityMedium, VulnerabilitySeverityHigh, VulnerabilitySeverityCritical:
	default:
		return nil, nil, fmt.Errorf("forward: invalid CVE severity %q", options.Severity)
	}
	switch options.Age {
	case "", CVEAgeMonth, CVEAgeYear, CVEAgeOlder:
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
	out := new(DeviceVulnerabilities)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}
