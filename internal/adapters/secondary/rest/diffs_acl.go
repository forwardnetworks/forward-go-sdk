package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// ACLDevices lists the devices whose ACLs changed, each with its change counts.
// GET /api/diffs/{a}/{b}/acl (getAclDiffStats, appserver controller, Preview;
// VIEW_ACL_DIFFS). It carries counts, not rules: DeviceACL lists a device's
// entries. Count with DiffACL is the device total without this body.
func (s *Diffs) ACLDevices(ctx context.Context, snapshotA, snapshotB string) ([]ports.ACLDiffDevice, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := diffsPath(snapshotA, snapshotB, "acl")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	out := listResponse[ports.ACLDiffDevice]{Keys: []string{"deviceInfos"}}
	resp, err := s.client.Do(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out.Items, resp, nil
}

// DeviceACL lists one device's ACL entries that were added, deleted or
// modified between two snapshots. GET /api/diffs/{a}/{b}/acl/devices/{device}
// (getDeviceAclDiff, appserver controller, Preview; VIEW_ACL_DIFFS, and the
// caller must be allowed to see the device). Entries carry the rule's name,
// action, match conditions and the config lines it came from (see
// ACLDiffEntry).
func (s *Diffs) DeviceACL(ctx context.Context, snapshotA, snapshotB, device string) (*ports.DeviceACLDiff, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if device = strings.TrimSpace(device); device == "" {
		return nil, nil, errors.New("forward: device name is required")
	}
	path, err := diffsPath(snapshotA, snapshotB, "acl/devices/"+url.PathEscape(device))
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	var out ports.DeviceACLDiff
	resp, err := s.client.Do(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return &out, resp, nil
}
