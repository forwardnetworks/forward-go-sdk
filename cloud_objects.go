package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// CloudObject is one object of a cloud account in a snapshot -- a VPC, subnet,
// route table, security group, instance and so on (CloudObjectMetadata, a
// sealed set of per-provider types for AWS, Azure, GCP, IBM and Alkira). The
// fields every provider shares are typed; Raw holds the whole object as
// Forward sent it, including each provider's own fields (blocks, IPs, tags,
// peering ends, collection error) and the location IDs, which are left raw.
type CloudObject struct {
	ID          string          `json:"id"`
	Name        string          `json:"name,omitempty"`
	AccountName string          `json:"accountName,omitempty"`
	Type        string          `json:"type,omitempty"`
	VPCs        []string        `json:"vpcs,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the shared fields and keeps the whole object in Raw.
func (o *CloudObject) UnmarshalJSON(data []byte) error {
	type plain CloudObject
	var out plain
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*o = CloudObject(out)
	o.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// CloudObjectPage is one page of a snapshot's cloud objects. Total counts all
// of them, not just this page.
type CloudObjectPage struct {
	CloudObjects []CloudObject `json:"cloudObjects"`
	Total        int           `json:"totalCloudObjects"`
}

// CloudObjectListOptions picks the snapshot and the page. An empty SnapshotID
// uses the latest processed snapshot. Limit zero returns everything from
// Offset; Forward applies no default page size.
type CloudObjectListOptions struct {
	SnapshotID string
	Offset     int
	Limit      int
}

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
func (s *NetworksService) CloudObjects(ctx context.Context, networkID string, options CloudObjectListOptions) (*CloudObjectPage, *Response, error) {
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
	out := new(CloudObjectPage)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// CloudObjectInfo is one cloud object with the topology it covers and, for an
// object Forward models as a device, that device's info. Both are left raw: their
// shapes are large and provider-specific.
type CloudObjectInfo struct {
	CloudType       string          `json:"cloudType,omitempty"`
	Type            string          `json:"type,omitempty"`
	ID              string          `json:"id"`
	Name            string          `json:"name,omitempty"`
	CoveredTopology json.RawMessage `json:"coveredTopo,omitempty"`
	DeviceInfo      json.RawMessage `json:"deviceInfo,omitempty"`
}

// CloudObject returns one cloud object by the provider's own ID (rtb-..., sg-...).
// GET /api/networks/{networkId}/cloud-objects/{objectId}
// (VIEW_NETWORK_AND_SNAPSHOTS). An empty snapshotID uses the latest processed
// snapshot. Preview.
func (s *NetworksService) CloudObject(ctx context.Context, networkID, objectID, snapshotID string) (*CloudObjectInfo, *Response, error) {
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
	out := new(CloudObjectInfo)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// CloudObjectFile is one stored file behind a cloud object: its ID and name
// (Forward sends the name as "fileName"). Raw holds the whole entry.
type CloudObjectFile struct {
	ID       Identifier      `json:"id"`
	FileName string          `json:"fileName"`
	Raw      json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the ID and name and keeps the whole entry in Raw.
func (f *CloudObjectFile) UnmarshalJSON(data []byte) error {
	type plain CloudObjectFile
	var out plain
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*f = CloudObjectFile(out)
	f.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// CloudObjectFilesOptions picks the snapshot and whether to include the files
// as collected. By default only the files Forward generated for the object are
// listed.
type CloudObjectFilesOptions struct {
	SnapshotID string
	AllFiles   bool
}

// CloudObjectFiles lists a cloud object's stored files. For an object that
// exists but has none, Forward returns a single dummy entry for a file that does
// not exist (DeviceState PROPERTIES), so do not treat a non-empty list as proof
// of content. GET /api/networks/{networkId}/cloud-objects/{objectId}/files
// (VIEW_COLLECTED_FILES; the snapshot must have reached the STORED_FILES
// stage). Preview.
func (s *NetworksService) CloudObjectFiles(ctx context.Context, networkID, objectID string, options CloudObjectFilesOptions) ([]CloudObjectFile, *Response, error) {
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
	var out []CloudObjectFile
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
