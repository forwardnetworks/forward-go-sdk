package ports

import (
	"net/url"
)

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
