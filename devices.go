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
	return s.client.Do(req, dst)
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
