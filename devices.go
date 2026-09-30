package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// DevicesService inspects modeled devices and their collected files.
type DevicesService service

// Device is a modeled device in a processed snapshot.
type Device struct {
	Name            string   `json:"name"`
	DisplayName     string   `json:"displayName,omitempty"`
	SourceName      string   `json:"sourceName,omitempty"`
	Type            string   `json:"type,omitempty"`
	Vendor          string   `json:"vendor,omitempty"`
	Model           string   `json:"model,omitempty"`
	Platform        string   `json:"platform,omitempty"`
	OSVersion       string   `json:"osVersion,omitempty"`
	ManagementIPs   []string `json:"managementIps,omitempty"`
	CollectionError string   `json:"collectionError,omitempty"`
	ProcessingError string   `json:"processingError,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	LocationID      string   `json:"locationId,omitempty"`
}

// DeviceListOptions filters modeled devices. ExtraQuery carries fields added
// by newer Forward versions.
type DeviceListOptions struct {
	SnapshotID      string
	Name            *string
	DisplayName     *string
	SourceName      *string
	Type            *string
	Vendor          *string
	Model           *string
	Platform        *string
	OSVersion       *string
	CollectionError *string
	ProcessingError *string
	Skip            *int32
	Limit           *int32
	With            []string
	ExtraQuery      url.Values
}

// DeviceFile is one raw file collected from a device.
type DeviceFile struct {
	Name    string `json:"name"`
	Bytes   int64  `json:"bytes,omitempty"`
	Command string `json:"command,omitempty"`
}

// List returns modeled devices for a network snapshot.
func (s *DevicesService) List(ctx context.Context, networkID string, options DeviceListOptions) ([]Device, *Response, error) {
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
	var devices []Device
	resp, err := s.client.Do(req, &devices)
	return devices, resp, err
}

// Get returns one modeled device.
func (s *DevicesService) Get(ctx context.Context, networkID, deviceName, snapshotID string, with ...string) (*Device, *Response, error) {
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
	device := new(Device)
	resp, err := s.client.Do(req, device)
	return device, resp, err
}

// MissingDevice is a device Forward did not model but inferred from its
// modeled neighbors' CDP, LLDP, iBGP or OSPF data
// (com.forwardnetworks.cv.snapshot.MissingDevice). Name is one it could be
// added under; Type is the DeviceConnType it would be modeled as (UNKNOWN when
// undetermined); Vendor is empty when unknown; Neighbors names the modeled
// devices that reported it; DiscoveryMethod is empty when not recorded.
type MissingDevice struct {
	Name            string   `json:"name"`
	IPAddresses     []string `json:"ipAddresses,omitempty"`
	Type            string   `json:"type,omitempty"`
	Vendor          string   `json:"vendor,omitempty"`
	Neighbors       []string `json:"neighbors,omitempty"`
	DiscoveryMethod string   `json:"discoveryMethod,omitempty"`
}

// Missing returns the devices a snapshot's neighbors see but Forward does not
// model -- usually ones that were never added or failed collection. GET
// /api/networks/{networkId}/missing-devices?snapshotId= (DeviceController.
// getMissingDevices, published; served on primary 15398425a69 and stable
// 67e89c87124). An empty snapshotID lets Forward pick the latest processed
// snapshot.
func (s *DevicesService) Missing(ctx context.Context, networkID, snapshotID string) ([]MissingDevice, *Response, error) {
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
	result := listResponse[MissingDevice]{Keys: []string{"devices"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// ListFiles lists raw collected files for a device.
func (s *DevicesService) ListFiles(ctx context.Context, networkID, deviceName, snapshotID string) ([]DeviceFile, *Response, error) {
	path, err := modeledDevicePath(networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	path += "/files"
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID != "" {
		path += "?" + url.Values{"snapshotId": []string{snapshotID}}.Encode()
	}
	var envelope struct {
		Files []DeviceFile `json:"files"`
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &envelope)
	return envelope.Files, resp, err
}

// DownloadFile streams a raw collected device file.
func (s *DevicesService) DownloadFile(ctx context.Context, networkID, deviceName, fileName, snapshotID string, dst io.Writer) (*Response, error) {
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
func (s *DevicesService) DownloadFileHead(ctx context.Context, networkID, deviceName, fileName, snapshotID string, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *Response, err error) {
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

func (s *DevicesService) fileRequest(ctx context.Context, networkID, deviceName, fileName, snapshotID string, dst io.Writer) (*http.Request, error) {
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
	req.Header.Set("Accept", "application/octet-stream, text/plain")
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
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	if deviceName = strings.TrimSpace(deviceName); deviceName == "" {
		return "", errors.New("forward: device name is required")
	}
	return path + "/devices/" + url.PathEscape(deviceName), nil
}

func deviceQuery(options DeviceListOptions) url.Values {
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
