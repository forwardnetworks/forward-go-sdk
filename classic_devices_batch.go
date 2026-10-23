package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// batchDeviceNames trims and checks a list of device names for the batch routes.
func batchDeviceNames(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, errors.New("forward: at least one device name is required")
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name == "" {
			return nil, errors.New("forward: a blank device name in the batch")
		}
		out = append(out, name)
	}
	return out, nil
}

// PatchMany applies ONE patch to many classic devices in a single request:
// PATCH /api/networks/{networkId}/classic-devices with {"names": [...],
// "update": <patch>} (ClassicDeviceController.patchClassicDevices, published as
// ClassicDevicesPatch; MANAGE_COLLECTION_SOURCES). It is the way to change
// thousands of devices without thousands of PATCH calls: Forward applies it in
// one transaction, so it either changes every named device or none (a bad
// credential or jump server id, a collector that does not exist, or a
// conflict fails the whole batch). Names are matched case-insensitively, and a
// name that matches no device is skipped silently, so compare with List when
// that matters. The same patch goes to every device: per-device values need
// per-device Patch calls (or PutBatch). patch.Clear and the pointer fields work
// exactly as in Patch, except Name, which PatchMany refuses. There is no response body.
func (s *ClassicDevicesService) PatchMany(ctx context.Context, networkID string, names []string, patch ClassicDevicePatch) (*Response, error) {
	if patch.Name != nil {
		return nil, errors.New("forward: PatchMany cannot rename devices (Forward answers 500 to a batch that renames); use Patch per device")
	}
	names, err := batchDeviceNames(names)
	if err != nil {
		return nil, err
	}
	path, err := classicDevicesPath(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, struct {
		Names  []string           `json:"names"`
		Update ClassicDevicePatch `json:"update"`
	}{names, patch})
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "ClassicDevices.PatchMany")
	return s.client.Do(req, nil)
}

// GetMany reads the named classic devices in one request, in place of one Get
// per device or a List of the whole network: POST
// /api/networks/{networkId}/classic-devices?action=getBatch
// (ClassicDeviceController.getSpecificClassicDevices, published;
// VIEW_COLLECTION_SOURCES) with {"names": [...]}. with is the same list as in
// List ("tags", "testResult" and so on). A name that matches no device is left
// out of the result rather than failing the call, so check the count when
// every device must exist.
func (s *ClassicDevicesService) GetMany(ctx context.Context, networkID string, names []string, with ...string) ([]ClassicDevice, *Response, error) {
	names, err := batchDeviceNames(names)
	if err != nil {
		return nil, nil, err
	}
	path, err := classicDevicesPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"action": []string{"getBatch"}}
	for _, value := range with {
		query.Add("with", value)
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?"+query.Encode(), struct {
		Names []string `json:"names"`
	}{names})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.GetMany")
	result := listResponse[ClassicDevice]{Keys: []string{"devices", "classicDevices", "items", "data", "results"}, AllowSingle: true}
	resp, err := s.client.doRequired(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
}
