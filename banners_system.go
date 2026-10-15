package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

// SystemAnnouncement is the banner Forward's operators post to every user of a
// SaaS deployment (web Announcement record: a message of at most 120
// characters, an optional absolute link, and when it was created).
type SystemAnnouncement struct {
	Message   string    `json:"message"`
	LinkURL   string    `json:"linkUrl,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Announcement reads the current system announcement, or nil when none is
// posted. GET /api/system-announcement (SaasAnnouncementController, Preview;
// any signed-in user). The route exists on SaaS only (@ProfileCriteria(not =
// ON_PREM)); an on-premises deployment answers with its typed
// ErrEndpointNotServed 404, which is "no announcements possible", not a
// failure. Posting and deleting are operator-only and not modelled.
func (s *BannersService) Announcement(ctx context.Context) (*SystemAnnouncement, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/system-announcement", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Banners.Announcement")
	var body struct {
		Announcement *SystemAnnouncement `json:"announcement"`
	}
	resp, err := s.client.doRequired(req, &body)
	if err != nil {
		return nil, resp, err
	}
	return body.Announcement, resp, nil
}

// MaintenanceWindow is a SaaS deployment's scheduled upgrade window (Forward's
// MaintenanceWindow record; both ends are epoch milliseconds on the wire).
type MaintenanceWindow struct {
	Start time.Time
	End   time.Time
}

// UnmarshalJSON reads {"start": <epoch ms>, "end": <epoch ms>}.
// A JSON null leaves the window untouched, and an end that is absent stays the zero time.
func (w *MaintenanceWindow) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	var wire struct {
		Start *float64 `json:"start"`
		End   *float64 `json:"end"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	millis := func(v *float64) time.Time {
		if v == nil {
			return time.Time{}
		}
		return time.UnixMilli(int64(*v)).UTC()
	}
	w.Start, w.End = millis(wire.Start), millis(wire.End)
	return nil
}

// MarshalJSON writes the same epoch-millisecond shape UnmarshalJSON reads.
func (w MaintenanceWindow) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	}{w.Start.UnixMilli(), w.End.UnixMilli()})
}

// Critical state names Forward's /api/critical-states can return. Which appear
// depends on the deployment: the common ones everywhere, the storage ones on
// premises only, MAINTENANCE_WINDOW on SaaS only.
const (
	CriticalCollectorOutdated          = "COLLECTOR_OUTDATED"
	CriticalLicenseCCHExhausted        = "LICENSE_CCH_EXHAUSTED"
	CriticalLicenseCCHGracePeriodEnded = "LICENSE_CCH_GRACE_PERIOD_ENDED"
	CriticalLowStorage                 = "LOW_STORAGE"
	CriticalNoStorage                  = "NO_STORAGE"
	CriticalNoBackupStorage            = "NO_BACKUP_STORAGE"
	CriticalMaintenanceWindow          = "MAINTENANCE_WINDOW"
	CriticalUnlicensed                 = "UNLICENSED"
	CriticalLicenseExpired             = "LICENSE_EXPIRED"
	CriticalLicenseNotStartedYet       = "LICENSE_NOT_STARTED_YET"
)

// CommonCriticalStateNames are returned by every deployment.
var CommonCriticalStateNames = []string{CriticalCollectorOutdated, CriticalLicenseCCHExhausted, CriticalLicenseCCHGracePeriodEnded}

// OnPremCriticalStateNames are returned in addition on an on-premises deployment.
var OnPremCriticalStateNames = []string{CriticalLowStorage, CriticalNoStorage, CriticalNoBackupStorage}

// SaasCriticalStateNames are returned in addition on a SaaS deployment.
var SaasCriticalStateNames = []string{CriticalMaintenanceWindow}

// CriticalStates is /api/critical-states: the org-wide conditions the UI turns
// into its top banner. A false or absent field means the state is not active.
// Reported lists the state names this deployment actually sent, so an absent
// name (the state does not exist here) is distinguishable from a false one.
//
// The license-status states (UNLICENSED, LICENSE_EXPIRED,
// LICENSE_NOT_STARTED_YET) are NOT in this response: the UI derives them from
// the org's license status. LicenseStatusCriticalState does the same mapping
// from Licensing.TierAndStatus.
type CriticalStates struct {
	CollectorOutdated          bool
	LicenseCCHExhausted        bool
	LicenseCCHGracePeriodEnded bool
	LowStorage                 bool
	NoStorage                  bool
	NoBackupStorage            bool
	// MaintenanceWindow is the scheduled upgrade window, or nil when none is
	// scheduled (SaaS reports MAINTENANCE_WINDOW as null then).
	MaintenanceWindow *MaintenanceWindow
	Reported          []string
	// Unknown holds the raw value of every state name this SDK does not model
	// (a future build's new critical state), so a consumer can still read it.
	Unknown map[string]json.RawMessage
}

// MarshalJSON writes the wire shape UnmarshalJSON reads, so a value round-trips.
func (c CriticalStates) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	for _, name := range c.Reported {
		if raw, ok := c.Unknown[name]; ok {
			out[name] = raw
		}
	}
	for name, v := range map[string]bool{
		CriticalCollectorOutdated: c.CollectorOutdated, CriticalLicenseCCHExhausted: c.LicenseCCHExhausted,
		CriticalLicenseCCHGracePeriodEnded: c.LicenseCCHGracePeriodEnded, CriticalLowStorage: c.LowStorage,
		CriticalNoStorage: c.NoStorage, CriticalNoBackupStorage: c.NoBackupStorage,
	} {
		if c.reports(name) {
			out[name] = v
		}
	}
	if c.reports(CriticalMaintenanceWindow) {
		if c.MaintenanceWindow == nil {
			out[CriticalMaintenanceWindow] = nil
		} else {
			out[CriticalMaintenanceWindow] = c.MaintenanceWindow
		}
	}
	return json.Marshal(out)
}

func (c CriticalStates) reports(name string) bool {
	for _, reported := range c.Reported {
		if reported == name {
			return true
		}
	}
	return false
}

// criticalStatePriority is the UI's order, highest first (core/critical-states.svelte.ts).
var criticalStatePriority = []string{
	CriticalUnlicensed, CriticalLicenseExpired, CriticalLicenseNotStartedYet, CriticalLicenseCCHGracePeriodEnded,
	CriticalNoStorage, CriticalNoBackupStorage, CriticalLowStorage, CriticalMaintenanceWindow,
	CriticalLicenseCCHExhausted, CriticalCollectorOutdated,
}

// UnmarshalJSON reads the state map. Unknown names are kept in Reported and Unknown.
func (c *CriticalStates) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = CriticalStates{}
	flag := func(name string, dst *bool) error {
		if value, ok := raw[name]; ok {
			return json.Unmarshal(value, dst)
		}
		return nil
	}
	for name, dst := range map[string]*bool{
		CriticalCollectorOutdated: &c.CollectorOutdated, CriticalLicenseCCHExhausted: &c.LicenseCCHExhausted,
		CriticalLicenseCCHGracePeriodEnded: &c.LicenseCCHGracePeriodEnded, CriticalLowStorage: &c.LowStorage,
		CriticalNoStorage: &c.NoStorage, CriticalNoBackupStorage: &c.NoBackupStorage,
	} {
		if err := flag(name, dst); err != nil {
			return err
		}
	}
	if value, ok := raw[CriticalMaintenanceWindow]; ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		c.MaintenanceWindow = new(MaintenanceWindow)
		if err := json.Unmarshal(value, c.MaintenanceWindow); err != nil {
			return err
		}
	}
	known := map[string]bool{
		CriticalCollectorOutdated: true, CriticalLicenseCCHExhausted: true, CriticalLicenseCCHGracePeriodEnded: true,
		CriticalLowStorage: true, CriticalNoStorage: true, CriticalNoBackupStorage: true, CriticalMaintenanceWindow: true,
	}
	for name, value := range raw {
		c.Reported = append(c.Reported, name)
		if !known[name] {
			if c.Unknown == nil {
				c.Unknown = map[string]json.RawMessage{}
			}
			c.Unknown[name] = value
		}
	}
	sort.Strings(c.Reported)
	return nil
}

// Active lists the active states reported by the server, highest priority
// first (the UI's order). License-status states are not included.
func (c CriticalStates) Active() []string {
	active := map[string]bool{
		CriticalCollectorOutdated: c.CollectorOutdated, CriticalLicenseCCHExhausted: c.LicenseCCHExhausted,
		CriticalLicenseCCHGracePeriodEnded: c.LicenseCCHGracePeriodEnded, CriticalLowStorage: c.LowStorage,
		CriticalNoStorage: c.NoStorage, CriticalNoBackupStorage: c.NoBackupStorage,
		CriticalMaintenanceWindow: c.MaintenanceWindow != nil,
	}
	var out []string
	for _, name := range criticalStatePriority {
		if active[name] {
			out = append(out, name)
		}
	}
	return out
}

// LicenseStatusCriticalState maps an org's license status (Licensing's
// OrgLicenseStatus: SOFTWARE_CENTRAL, EXEMPT, ACTIVE, NOT_ACTIVE_YET,
// EXPIRED_RECENTLY, EXPIRED; empty when the org has no license) to the
// critical state the UI shows for it, or "" when none applies. It mirrors the
// UI's own derivation; the server does not send these states.
func LicenseStatusCriticalState(status string) string {
	switch status {
	case "":
		return CriticalUnlicensed
	case "EXPIRED_RECENTLY", "EXPIRED":
		return CriticalLicenseExpired
	case "NOT_ACTIVE_YET":
		return CriticalLicenseNotStartedYet
	}
	return ""
}

// CriticalStates reads the org's critical states. GET /api/critical-states
// (NotificationController.getCriticalStates, Preview; any signed-in user).
func (s *BannersService) CriticalStates(ctx context.Context) (*CriticalStates, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/critical-states", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Banners.CriticalStates")
	out := new(CriticalStates)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
