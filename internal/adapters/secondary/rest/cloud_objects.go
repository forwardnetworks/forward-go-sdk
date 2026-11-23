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

func cloudObjectsPath(networkID, objectID string) (string, error) {
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	path += "/cloud-objects"
	if objectID == "" {
		return path, nil
	}
	if objectID = strings.TrimSpace(objectID); objectID == "" {
		return "", errors.New("forward: cloud object ID is required")
	}
	return path + "/" + url.PathEscape(objectID), nil
}

// CloudObjects lists the cloud objects collected in a snapshot, in Forward's
// order. GET /api/networks/{networkId}/cloud-objects (CloudObjectController;
// VIEW_NETWORK_AND_SNAPSHOTS). Preview: not in the published spec.
func (s *Networks) CloudObjects(ctx context.Context, networkID string, options ports.CloudObjectListOptions) (*ports.CloudObjectPage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if options.Offset < 0 || options.Limit < 0 {
		return nil, nil, errors.New("forward: cloud object offset and limit must not be negative")
	}
	path, err := cloudObjectsPath(networkID, "")
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{}
	setString(query, "snapshotId", options.SnapshotID)
	if options.Offset != 0 {
		query.Set("offset", strconv.Itoa(options.Offset))
	}
	if options.Limit != 0 {
		query.Set("limit", strconv.Itoa(options.Limit))
	}
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.CloudObjects")
	out := new(ports.CloudObjectPage)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// CloudObject returns one cloud object by the provider's own ID (rtb-..., sg-...).
// GET /api/networks/{networkId}/cloud-objects/{objectId}
// (VIEW_NETWORK_AND_SNAPSHOTS). An empty snapshotID uses the latest processed
// snapshot. Preview.
func (s *Networks) CloudObject(ctx context.Context, networkID, objectID, snapshotID string) (*ports.CloudObjectInfo, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(objectID) == "" {
		return nil, nil, errors.New("forward: cloud object ID is required")
	}
	path, err := cloudObjectsPath(networkID, objectID)
	if err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID != "" {
		path += "?" + url.Values{"snapshotId": []string{snapshotID}}.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.CloudObject")
	out := new(ports.CloudObjectInfo)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// CloudObjectFiles lists a cloud object's stored files. For an object that
// exists but has none, Forward returns a single dummy entry for a file that does
// not exist (DeviceState PROPERTIES), so do not treat a non-empty list as proof
// of content. GET /api/networks/{networkId}/cloud-objects/{objectId}/files
// (VIEW_COLLECTED_FILES; the snapshot must have reached the STORED_FILES
// stage). Preview.
func (s *Networks) CloudObjectFiles(ctx context.Context, networkID, objectID string, options ports.CloudObjectFilesOptions) ([]ports.CloudObjectFile, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(objectID) == "" {
		return nil, nil, errors.New("forward: cloud object ID is required")
	}
	path, err := cloudObjectsPath(networkID, objectID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{}
	setString(query, "snapshotId", options.SnapshotID)
	if options.AllFiles {
		query.Set("generatedOnly", "false")
	}
	path += "/files"
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.CloudObjectFiles")
	var out []ports.CloudObjectFile
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
