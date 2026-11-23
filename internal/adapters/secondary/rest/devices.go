package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// DevicesService inspects modeled devices and their collected files.
type Devices service

// List returns modeled devices for a network snapshot.
func (s *Devices) List(ctx context.Context, networkID string, options ports.DeviceListOptions) ([]ports.Device, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	query := deviceQuery(options)
	path += "/devices"
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	var devices []ports.Device
	resp, err := s.client.Do(req, &devices)
	return devices, resp, err
}

// Get returns one modeled device.
func (s *Devices) Get(ctx context.Context, networkID, deviceName, snapshotID string, with ...string) (*ports.Device, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := modeledDevicePath(networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{}
	setString(query, "snapshotId", snapshotID)
	for _, value := range with {
		query.Add("with", value)
	}
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	device := new(ports.Device)
	resp, err := s.client.Do(req, device)
	return device, resp, err
}

// Missing returns the devices a snapshot's neighbors see but Forward does not
// model -- usually ones that were never added or failed collection. GET
// /api/networks/{networkId}/missing-devices?snapshotId= (DeviceController.
// getMissingDevices, published; served on primary 15398425a69 and stable
// 67e89c87124). An empty snapshotID lets Forward pick the latest processed
// snapshot.
func (s *Devices) Missing(ctx context.Context, networkID, snapshotID string) ([]ports.MissingDevice, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	path += "/missing-devices"
	query := url.Values{}
	setString(query, "snapshotId", snapshotID)
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Devices.Missing")
	result := listResponse[ports.MissingDevice]{Keys: []string{"devices"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// ListFiles lists raw collected files for a device.
func (s *Devices) ListFiles(ctx context.Context, networkID, deviceName, snapshotID string) ([]ports.DeviceFile, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := modeledDevicePath(networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	path += "/files"
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID != "" {
		path += "?" + url.Values{"snapshotId": []string{snapshotID}}.Encode()
	}
	var envelope struct {
		Files []ports.DeviceFile `json:"files"`
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &envelope)
	return envelope.Files, resp, err
}

// DownloadFile streams a raw collected device file.
func (s *Devices) DownloadFile(ctx context.Context, networkID, deviceName, fileName, snapshotID string, dst io.Writer) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	req, err := s.fileRequest(ctx, networkID, deviceName, fileName, snapshotID, dst)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, dst)
}

// DownloadFileHead writes at most maxBytes of a collected device file to dst
// and stops, reporting whether the file was longer. Forward does not honor
// Range on this route (its handler returns an InputStreamResource, which
// Spring MVC serves whole), so the cap is applied here: the response body is
// closed once maxBytes have arrived, which stops the transfer rather than
// discarding the rest. A truncated read is not an error.
func (s *Devices) DownloadFileHead(ctx context.Context, networkID, deviceName, fileName, snapshotID string, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *ports.Response, err error) {
	if err := (*service)(s).failClosed(); err != nil {
		return 0, false, nil, err
	}
	if maxBytes <= 0 {
		return 0, false, nil, errors.New("forward: maxBytes must be positive")
	}
	req, err := s.fileRequest(ctx, networkID, deviceName, fileName, snapshotID, dst)
	if err != nil {
		return 0, false, nil, err
	}
	req = markOperation(req, "Devices.DownloadFileHead")
	head := &headWriter{dst: dst, remaining: maxBytes}
	resp, err = s.client.Do(req, head)
	if errors.Is(err, errHeadFull) {
		return head.written, true, resp, nil
	}
	return head.written, false, resp, err
}

func (s *Devices) fileRequest(ctx context.Context, networkID, deviceName, fileName, snapshotID string, dst io.Writer) (*http.Request, error) {
	if strings.TrimSpace(fileName) == "" || dst == nil {
		return nil, errors.New("forward: device file name and destination writer are required")
	}
	path, err := modeledDevicePath(networkID, deviceName)
	if err != nil {
		return nil, err
	}
	path += "/files/" + url.PathEscape(strings.TrimSpace(fileName))
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID != "" {
		path += "?" + url.Values{"snapshotId": []string{snapshotID}}.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	// application/json too: Forward answers an error as JSON, and without it Spring can only say 406.
	req.Header.Set("Accept", "application/octet-stream, text/plain, application/json")
	return req, nil
}

var errHeadFull = errors.New("forward: download head is full")

// headWriter passes through up to remaining bytes, then fails the write so the
// copy stops and the body is closed.
type headWriter struct {
	dst       io.Writer
	remaining int64
	written   int64
}

func (h *headWriter) Write(p []byte) (int, error) {
	if h.remaining <= 0 {
		return 0, errHeadFull
	}
	chunk := p
	if int64(len(chunk)) > h.remaining {
		chunk = chunk[:h.remaining]
	}
	n, err := h.dst.Write(chunk)
	h.written += int64(n)
	h.remaining -= int64(n)
	if err != nil {
		return n, err
	}
	if n < len(p) {
		return n, errHeadFull
	}
	return n, nil
}

func modeledDevicePath(networkID, deviceName string) (string, error) {
	networkID, deviceName, err := ports.RequireDeviceRef(networkID, deviceName)
	if err != nil {
		return "", err
	}
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	return path + "/devices/" + url.PathEscape(deviceName), nil
}

func deviceQuery(options ports.DeviceListOptions) url.Values {
	query := cloneValues(options.ExtraQuery)
	setString(query, "snapshotId", options.SnapshotID)
	for key, value := range map[string]*string{
		"name": options.Name, "displayName": options.DisplayName, "sourceName": options.SourceName,
		"type": options.Type, "vendor": options.Vendor, "model": options.Model, "platform": options.Platform,
		"osVersion": options.OSVersion, "collectionError": options.CollectionError, "processingError": options.ProcessingError,
	} {
		if value != nil {
			query.Set(key, *value)
		}
	}
	if options.Skip != nil {
		query.Set("skip", strconv.FormatInt(int64(*options.Skip), 10))
	}
	setInt32(query, "limit", options.Limit)
	if options.With != nil {
		query.Del("with")
		for _, value := range options.With {
			query.Add("with", value)
		}
	}
	return query
}
