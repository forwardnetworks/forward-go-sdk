package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func deviceInterfacesPath(networkID, deviceName string) (string, error) {
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		return "", errors.New("forward: device name is required")
	}
	base, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	return base + "/devices/" + url.PathEscape(deviceName) + "/interfaces", nil
}

// Interfaces lists a device's interfaces, sorted by name. GET /api/networks/{id}/devices/{device}/interfaces
// (DeviceController, published in 26.10 as experimental; 409 while Forward processes the snapshot, see ErrSnapshotNotProcessed; the same path served an unpublished route with a different
// shape: type was a display name, VLANs were strings, neighbors were "device port" strings and came without a request).
func (s *Devices) Interfaces(ctx context.Context, networkID, deviceName string, options ports.DeviceInterfaceOptions) ([]ports.DeviceInterface, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := deviceInterfacesPath(networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+deviceInterfaceOptionsQuery(options), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Devices.Interfaces")
	var out struct {
		Interfaces []ports.DeviceInterface `json:"interfaces"`
	}
	resp, err := s.client.doRequired(req, &out)
	return out.Interfaces, resp, err
}

// Interface returns one of a device's interfaces by name or alias (case-sensitive; a "/" in it is percent-encoded).
// GET /api/networks/{id}/devices/{device}/interfaces/{interface}; experimental, see Interfaces.
func (s *Devices) Interface(ctx context.Context, networkID, deviceName, interfaceName string, options ports.DeviceInterfaceOptions) (*ports.DeviceInterface, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if interfaceName = strings.TrimSpace(interfaceName); interfaceName == "" {
		return nil, nil, errors.New("forward: interface name is required")
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := deviceInterfacesPath(networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/"+url.PathEscape(interfaceName)+deviceInterfaceOptionsQuery(options), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Devices.Interface")
	out := new(ports.DeviceInterface)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// Diff reports each device added, removed or modified between the before and after snapshots of a network; unchanged
// devices are omitted and devices match by name ignoring case. GET /api/networks/{id}/device-diffs/{before}/{after}
// (DeviceController, published in 26.10 as experimental). Answers 409 (ErrSnapshotNotProcessed) while Forward is processing either snapshot.
func (s *Devices) Diff(ctx context.Context, networkID, beforeSnapshotID, afterSnapshotID string, options ports.DeviceDiffOptions) (*ports.DeviceDiffs, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	beforeSnapshotID, afterSnapshotID = strings.TrimSpace(beforeSnapshotID), strings.TrimSpace(afterSnapshotID)
	if beforeSnapshotID == "" || afterSnapshotID == "" {
		return nil, nil, errors.New("forward: before and after snapshot IDs are required")
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	base, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	q := url.Values{}
	setString(q, "name", options.Name)
	if len(options.ChangeTypes) > 0 {
		q.Set("changeType", strings.Join(options.ChangeTypes, ","))
	}
	if len(options.With) > 0 {
		q.Set("with", strings.Join(options.With, ","))
	}
	if options.Limit != nil {
		q.Set("limit", strconv.FormatInt(int64(*options.Limit), 10))
	}
	path := base + "/device-diffs/" + url.PathEscape(beforeSnapshotID) + "/" + url.PathEscape(afterSnapshotID)
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Devices.Diff")
	out := new(ports.DeviceDiffs)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

func deviceInterfaceOptionsQuery(o ports.DeviceInterfaceOptions) string {
	q := url.Values{}
	setString(q, "snapshotId", o.SnapshotID)
	if o.WithNeighbors {
		q.Add("with", "neighbors")
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}
