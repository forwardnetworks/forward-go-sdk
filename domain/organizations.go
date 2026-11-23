package domain

import (
	"encoding/json"
)

// Organization is a Forward tenant. Raw retains version-specific response
// fields returned by administrative builds.
type Organization struct {
	ID            Identifier `json:"id"`
	Name          string     `json:"name"`
	Disabled      bool       `json:"disabled,omitempty"`
	SFDCAccountID string     `json:"sfdcAccountId,omitempty"`
	SFDCContactID string     `json:"sfdcContactId,omitempty"`
	// Raw carries fields this SDK version does not model, so an object read
	// from a newer appserver and written back does not silently lose them.
	Raw map[string]json.RawMessage `json:"-"`
}

func (o *Organization) UnmarshalJSON(data []byte) error {
	type plain Organization
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*o = Organization(value)
	o.Raw = raw
	return nil
}

// DeleteOrgOptions tunes DeleteOrgCleanly.
type DeleteOrgOptions struct {
	// Networks are the ids of the org's networks to delete first, one by one, so each failure is reported by id. When nil they are listed
	// with Admin.ListNetworks (GET /api/admin/networks), which needs a client built with AuthModeService. Whatever this misses is swept by
	// DeleteOrgNetworks before the org is deleted, so a short list cannot orphan a network.
	Networks []string
	// SkipUserCheck skips the check that the org has no users. With an AuthModeService client DeleteOrgCleanly lists users first and refuses
	// before disabling anything, because Forward will not delete an org that still has users. Other clients cannot list users, so the
	// check is skipped for them: delete the users first, or the call ends with the org disabled and its networks gone.
	SkipUserCheck bool
	// KeepOrg leaves the org disabled and not deleted, after its networks are deleted.
	KeepOrg bool
}

// DeleteOrgResult says what DeleteOrgCleanly did. Failed holds one error per network that could not be deleted; when it is not empty
// the org was NOT deleted.
type DeleteOrgResult struct {
	Disabled bool
	Deleted  []string
	Failed   map[string]error
	Org      *Organization
}
