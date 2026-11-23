package domain

import (
	"encoding/json"
)

// Connection test results (Forward's ConnectionStatus).
const (
	FwdAppConnectionSuccess          = "SUCCESS"
	FwdAppConnectionAuthFailed       = "AUTH_FAILED"
	FwdAppConnectionConnectionFailed = "CONNECTION_FAILED"
)

// FwdAppConnectionTest is the answer of Test (ConnectionTestResponse). A failed connection is a 200 whose Status says so, not an HTTP error.
type FwdAppConnectionTest struct {
	Status string `json:"status"`
}

// OK reports Status SUCCESS.
func (t *FwdAppConnectionTest) OK() bool { return t != nil && t.Status == FwdAppConnectionSuccess }

// FwdAppProxy is the organization's proxy settings for reaching fwd.app (StoredFwdAppProxy, FwdAppProxy). The credential's password is never
// returned; only its username is. Enabled false means a direct connection. Attribution fields (who set it, and when) are in Raw.
type FwdAppProxy struct {
	Enabled           bool   `json:"enabled"`
	URL               string `json:"url,omitempty"`
	VerifyCertificate *bool  `json:"verifyCertificate,omitempty"`
	EnableAuth        *bool  `json:"enableAuth,omitempty"`
	Username          string `json:"username,omitempty"`
	// Raw is the whole object, including the attribution Forward unwraps into it.
	Raw json.RawMessage `json:"-"`
}

// CVE index update errors (Forward's CveIndexUpdateError).
const (
	CVEIndexUpdateConnectionFailed = "CONNECTION_FAILED"
	CVEIndexUpdateInstallFailed    = "INSTALL_FAILED"
)

// CVEIndexUpdate is the answer of UpdateFromSoftwareCentral (CveIndexUpdateResponse): Error is empty on success, and a failure is a 200 whose
// Error names it, CVEIndexUpdateConnectionFailed (could not reach Software Central or download) or CVEIndexUpdateInstallFailed (downloaded
// but could not install).
type CVEIndexUpdate struct {
	Error string `json:"error,omitempty"`
}

// OK reports that no error came back. It does not say whether a newer index was installed: Forward answers the same when the index was
// already up to date and nothing was downloaded.
func (u *CVEIndexUpdate) OK() bool { return u != nil && u.Error == "" }
