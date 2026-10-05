package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// DeviceTagsService manages network-scoped device tag definitions and
// assignments.
type DeviceTagsService service

type DeviceTag struct {
	Name    string   `json:"name"`
	Color   string   `json:"color,omitempty"`
	Devices []string `json:"devices,omitempty"`
}

// AddBatch creates tag definitions. Forward ignores definitions that already
// exist.
func (s *DeviceTagsService) AddBatch(ctx context.Context, networkID string, tags []DeviceTag) (*Response, error) {
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
func (s *DeviceTagsService) AddBatchTo(ctx context.Context, networkID string, devices, tags []string) (*Response, error) {
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
func (s *DeviceTagsService) RemoveBatchFrom(ctx context.Context, networkID string, devices, tags []string) (*Response, error) {
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
func (s *DeviceTagsService) List(ctx context.Context, networkID, with string) ([]DeviceTag, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, _ := networkPath(networkID)
	path += "/device-tags"
	if with = strings.TrimSpace(with); with != "" {
		path += "?" + url.Values{"with": []string{with}}.Encode()
	}
	result := listResponse[DeviceTag]{Keys: []string{"tags", "items"}}
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

// DeviceTagPatch changes a tag definition; nil fields are left alone. Name
// renames it everywhere (Forward answers 409 if another tag in the network
// already has the name), and Color is "#rrggbb" in either case.
type DeviceTagPatch struct {
	Name  *string
	Color *string
}

// UpdateTag renames a device tag or changes its colour, and returns it. PATCH
// /api/networks/{networkId}/device-tags/{tagName} (updateDeviceTag;
// MANAGE_COLLECTION_SOURCES).
//
// Forward's service documents this as "creates or updates", so patching a tag
// that does not exist creates it with the patched values; check List first if
// that matters. Renaming moves the tag's assignments with it. An empty patch is
// refused.
func (s *DeviceTagsService) UpdateTag(ctx context.Context, networkID, tag string, patch DeviceTagPatch) (*DeviceTag, *Response, error) {
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
	out := new(DeviceTag)
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
func (s *DeviceTagsService) DeleteTag(ctx context.Context, networkID, tag string) (*Response, error) {
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
	if isGone(err) {
		return resp, nil
	}
	return resp, err
}

var deviceTagColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (s *DeviceTagsService) tagPath(networkID, tag string) (string, error) {
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
