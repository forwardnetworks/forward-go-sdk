package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// SnapshotMetrics is Forward's per-snapshot collection and processing health
// (com.forwardnetworks.cv.metric.SnapshotMetrics), from
// GET /api/snapshots/{snapshotId}/metrics.
//
// The failure maps count devices (or endpoints) per error name:
// DeviceCollectionFailures is keyed by DeviceCollectionError
// (AUTHENTICATION_FAILED, AUTHORIZATION_FAILED, ...), DeviceProcessingFailures
// by DeviceProcessingError (PARSER_EXCEPTION, LICENSE_EXHAUSTED, DUPLICATE,
// ...). Error names are version-dependent, so they stay strings.
type SnapshotMetrics struct {
	SnapshotID Identifier `json:"snapshotId"`
	CreatedAt  string     `json:"createdAt,omitempty"`

	NumSuccessfulDevices          int `json:"numSuccessfulDevices"`
	NumCollectionFailureDevices   int `json:"numCollectionFailureDevices"`
	NumProcessingFailureDevices   int `json:"numProcessingFailureDevices"`
	NumSuccessfulEndpoints        int `json:"numSuccessfulEndpoints"`
	NumCollectionFailureEndpoints int `json:"numCollectionFailureEndpoints"`
	NumProcessingFailureEndpoints int `json:"numProcessingFailureEndpoints"`

	DeviceCollectionFailures   map[string]int `json:"deviceCollectionFailures,omitempty"`
	DeviceProcessingFailures   map[string]int `json:"deviceProcessingFailures,omitempty"`
	EndpointCollectionFailures map[string]int `json:"endpointCollectionFailures,omitempty"`
	EndpointProcessingFailures map[string]int `json:"endpointProcessingFailures,omitempty"`

	// Durations are milliseconds on the wire. Nil means Forward has none: no
	// collection data (an import), or processing that has not finished.
	CollectionDurationMillis *int64 `json:"collectionDuration,omitempty"`
	ProcessingDurationMillis *int64 `json:"processingDuration,omitempty"`

	// Index statuses are StatusData.Status values (SUCCESS, FAILURE,
	// CANCELED, ...).
	L2IndexingStatus         string `json:"l2IndexingStatus,omitempty"`
	HostComputationStatus    string `json:"hostComputationStatus,omitempty"`
	IPLocationIndexingStatus string `json:"ipLocationIndexingStatus,omitempty"`
	PathSearchIndexingStatus string `json:"pathSearchIndexingStatus,omitempty"`
	SearchIndexingStatus     string `json:"searchIndexingStatus,omitempty"`
	SnapshotState            string `json:"snapshotState,omitempty"`
}

// Metrics returns a snapshot's collection and processing health: device and
// endpoint success/failure counts, failures by error name, durations and index
// statuses. GET /api/snapshots/{snapshotId}/metrics (SnapshotController,
// served on primary 15398425a69 and stable 67e89c87124). Forward answers 404
// when the snapshot has no metrics, and ErrSnapshotNotProcessed before
// processing has started.
func (s *SnapshotsService) Metrics(ctx context.Context, snapshotID string) (*SnapshotMetrics, *Response, error) {
	path, err := snapshotSubPath(snapshotID, "metrics")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.Metrics")
	out := new(SnapshotMetrics)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// SnapshotException is one distinct exception Forward hit while building a
// snapshot (com.forwardnetworks.cv.snapshot.service.SnapshotException):
// ExceptionType is PARSING, MODELING, SNAPSHOT_GENERATION, REACHABILITY or
// SHERLOCK_INDEX; Devices names the devices it occurred on (empty for a
// snapshot-wide stage); Occurrences counts them.
type SnapshotException struct {
	StackTrace    string   `json:"stackTrace"`
	Occurrences   int      `json:"occurrences"`
	ExceptionType string   `json:"exceptionType"`
	Devices       []string `json:"devices,omitempty"`
}

// Exceptions returns the exceptions Forward recorded while parsing, modeling
// and indexing a snapshot. GET /api/snapshots/{snapshotId}/exceptions?view=json
// (SnapshotController.getExceptionsJson, served on primary 15398425a69 and
// stable 67e89c87124). It needs the DEBUG_SNAPSHOTS network permission; a
// caller without it gets a 403.
func (s *SnapshotsService) Exceptions(ctx context.Context, snapshotID string) ([]SnapshotException, *Response, error) {
	path, err := snapshotSubPath(snapshotID, "exceptions")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+url.Values{"view": []string{"json"}}.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.Exceptions")
	result := listResponse[SnapshotException]{Keys: []string{"exceptions"}}
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func snapshotSubPath(snapshotID, tail string) (string, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return "", errors.New("forward: snapshot ID is required")
	}
	return "/api/snapshots/" + url.PathEscape(snapshotID) + "/" + tail, nil
}
