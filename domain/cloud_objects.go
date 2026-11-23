package domain

import (
	"encoding/json"
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
