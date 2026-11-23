package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// DeviceTagsService manages network-scoped device tag definitions and
// assignments.
type DeviceTags service

// AddBatch creates tag definitions. Forward ignores definitions that already
// exist.
func (s *DeviceTags) AddBatch(ctx context.Context, networkID string, tags []ports.DeviceTag) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	if len(tags) == 0 {
		return nil, errors.New("forward: at least one device tag is required")
	}
	path, _ := networkPath(networkID)
	path += "/device-tags?" + url.Values{"action": []string{"addBatch"}}.Encode()
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, tags)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// AddBatchTo applies existing tag names to devices. It sends no snapshotId, so Forward writes it at the
// network's STAGING instant: an instant after every snapshot, used for timeline entities that apply to
// any future snapshot (NetworkInstant.staging). The devices must be collection sources (Forward validates
// unless told not to; this SDK always lets it). From Forward's source, not a live test: List with
// "devices" also sends no snapshotId and reads "active tags as of the staging instant"
// (DeviceTagsService.getAllActiveDeviceTags), so a tag applied here should show in that List at once,
// before any snapshot exists. A read AT a snapshot (not offered by this SDK) shows only what was active then.
func (s *DeviceTags) AddBatchTo(ctx context.Context, networkID string, devices, tags []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	devices = nonEmptyStrings(devices)
	tags = nonEmptyStrings(tags)
	if len(devices) == 0 || len(tags) == 0 {
		return nil, errors.New("forward: device names and tag names are required")
	}
	path, _ := networkPath(networkID)
	path += "/device-tags?" + url.Values{"action": []string{"addBatchTo"}}.Encode()
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, map[string][]string{"devices": devices, "tags": tags})
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// RemoveBatchFrom takes the named tags off the named devices (POST ?action=removeBatchFrom, operationId removeDeviceTagsFromDevices in
// Forward's published device-tags spec): the undo of AddBatchTo. It sends no snapshotId, so the change applies to the network's next
// snapshot and the devices must be collection sources, exactly as AddBatchTo behaves. Tag definitions are left in place; removing a tag
// from a device that does not carry it is not an error on the server.
func (s *DeviceTags) RemoveBatchFrom(ctx context.Context, networkID string, devices, tags []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	devices = nonEmptyStrings(devices)
	tags = nonEmptyStrings(tags)
	if len(devices) == 0 || len(tags) == 0 {
		return nil, errors.New("forward: device names and tag names are required")
	}
	path, _ := networkPath(networkID)
	path += "/device-tags?" + url.Values{"action": []string{"removeBatchFrom"}}.Encode()
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, map[string][]string{"devices": devices, "tags": tags})
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "DeviceTags.RemoveBatchFrom") // a POST that removes: the operation name is how a hook recognises it as destructive
	return s.client.Do(req, nil)
}

// List returns tag definitions. with is an optional appserver expansion such
// as "devices". Bare arrays and {"tags": [...]} are both accepted.
// With "devices" and no snapshot it reflects the staging instant, i.e. tags already applied by AddBatchTo and
// RemoveBatchFrom, whether or not a snapshot has been taken since.
func (s *DeviceTags) List(ctx context.Context, networkID, with string) ([]ports.DeviceTag, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, _ := networkPath(networkID)
	path += "/device-tags"
	if with = strings.TrimSpace(with); with != "" {
		path += "?" + url.Values{"with": []string{with}}.Encode()
	}
	result := listResponse[ports.DeviceTag]{Keys: []string{"tags", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	response, err := s.client.Do(req, &result)
	if err != nil {
		return nil, response, err
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Name < result.Items[j].Name })
	return result.Items, response, nil
}

// batchPath builds the addBatchTo / removeBatchFrom URL, with the snapshotId when the change is to take effect from a snapshot.
func (s *DeviceTags) batchRequest(ctx context.Context, networkID, snapshotID, action, operation string, devices, tags []string) (*ports.Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	devices = nonEmptyStrings(devices)
	tags = nonEmptyStrings(tags)
	if len(devices) == 0 || len(tags) == 0 {
		return nil, errors.New("forward: device names and tag names are required")
	}
	query := url.Values{"action": []string{action}}
	if snapshotID != "" {
		query.Set("snapshotId", snapshotID)
	}
	path, _ := networkPath(networkID)
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"/device-tags?"+query.Encode(), map[string][]string{"devices": devices, "tags": tags})
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, operation), nil)
}

// AddBatchToAt is AddBatchTo with the change taking effect from a snapshot (POST ?action=addBatchTo&snapshotId=; DeviceTagController
// addDeviceTagsToDevices, MANAGE_COLLECTION_SOURCES). Forward's spec: "the change will take effect starting from the identified network
// Snapshot", so an NQE read of that snapshot (device.tagNames) shows the tags at once, where AddBatchTo (no snapshot, the staging instant)
// shows them only from the next snapshot. The devices are validated against the SNAPSHOT's device and endpoint catalogs, not the network's
// collection sources, so a virtual system or an access point the collection-source form refuses ("Unrecognized device name(s)") is
// accepted if the snapshot has it; a name the snapshot lacks is refused with that same 400. From the spec and source, not a live test. The
// SDK never sends validateDevices=false.
func (s *DeviceTags) AddBatchToAt(ctx context.Context, networkID, snapshotID string, devices, tags []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(snapshotID) == "" {
		return nil, errors.New("forward: a snapshot ID is required (use AddBatchTo for the next snapshot)")
	}
	return s.batchRequest(ctx, networkID, strings.TrimSpace(snapshotID), "addBatchTo", "DeviceTags.AddBatchToAt", devices, tags)
}

// RemoveBatchFromAt is RemoveBatchFrom taking effect from a snapshot (POST ?action=removeBatchFrom&snapshotId=), validated against the
// snapshot's devices and endpoints like AddBatchToAt.
func (s *DeviceTags) RemoveBatchFromAt(ctx context.Context, networkID, snapshotID string, devices, tags []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(snapshotID) == "" {
		return nil, errors.New("forward: a snapshot ID is required (use RemoveBatchFrom for the next snapshot)")
	}
	return s.batchRequest(ctx, networkID, strings.TrimSpace(snapshotID), "removeBatchFrom", "DeviceTags.RemoveBatchFromAt", devices, tags)
}

// ListAt returns the tags with the devices that carried each as of a snapshot (GET /device-tags?with=devices&snapshotId=;
// getDeviceTagsWithDevices, VIEW_NETWORK_AND_SNAPSHOTS). Without a snapshot List reads the staging instant instead. Forward uses the
// snapshot to canonicalize the case of device names. Only the with=devices form takes a snapshot, so it is always sent.
func (s *DeviceTags) ListAt(ctx context.Context, networkID, snapshotID string) ([]ports.DeviceTag, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: a snapshot ID is required (use List for the staging instant)")
	}
	path, _ := networkPath(networkID)
	path += "/device-tags?" + url.Values{"with": []string{"devices"}, "snapshotId": []string{snapshotID}}.Encode()
	result := listResponse[ports.DeviceTag]{Keys: []string{"tags", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	response, err := s.client.Do(markOperation(req, "DeviceTags.ListAt"), &result)
	if err != nil {
		return nil, response, err
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Name < result.Items[j].Name })
	return result.Items, response, nil
}

// UpdateTag renames a device tag or changes its colour, and returns it. PATCH
// /api/networks/{networkId}/device-tags/{tagName} (updateDeviceTag;
// MANAGE_COLLECTION_SOURCES).
//
// Forward's service documents this as "creates or updates", so patching a tag
// that does not exist creates it with the patched values; check List first if
// that matters. Renaming moves the tag's assignments with it. An empty patch is
// refused.
func (s *DeviceTags) UpdateTag(ctx context.Context, networkID, tag string, patch ports.DeviceTagPatch) (*ports.DeviceTag, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if patch.Name == nil && patch.Color == nil {
		return nil, nil, errors.New("forward: a device tag patch must change something")
	}
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return nil, nil, errors.New("forward: a device tag cannot be renamed to nothing")
	}
	if patch.Color != nil && !deviceTagColor.MatchString(*patch.Color) {
		return nil, nil, fmt.Errorf("forward: device tag color %q must be #rrggbb", *patch.Color)
	}
	path, err := s.tagPath(networkID, tag)
	if err != nil {
		return nil, nil, err
	}
	body := map[string]string{}
	if patch.Name != nil {
		body["name"] = *patch.Name
	}
	if patch.Color != nil {
		body["color"] = *patch.Color
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, body)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DeviceTags.UpdateTag")
	out := new(ports.DeviceTag)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// DeleteTag deletes a tag definition AND removes it from every device across
// the network's whole timeline, not just the next snapshot (deleteDeviceTag).
// RemoveBatchFrom only takes a tag off named devices and leaves the
// definition. A tag that does not exist counts as success. DELETE
// /api/networks/{networkId}/device-tags/{tagName} (MANAGE_COLLECTION_SOURCES;
// 204). There is no undo: re-creating the tag does not restore its
// assignments.
func (s *DeviceTags) DeleteTag(ctx context.Context, networkID, tag string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := s.tagPath(networkID, tag)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "DeviceTags.DeleteTag")
	resp, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
		return resp, nil
	}
	return resp, err
}

var deviceTagColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (s *DeviceTags) tagPath(networkID, tag string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	if tag = strings.TrimSpace(tag); tag == "" {
		return "", errors.New("forward: device tag name is required")
	}
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	return path + "/device-tags/" + url.PathEscape(tag), nil
}
