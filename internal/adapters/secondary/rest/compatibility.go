package rest

import (
	"bytes"
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
)

// CompatibilityService exposes operation-specific typed passthroughs for
// facades whose public contract is the upstream status/headers/body.
// It is intentionally not an arbitrary Raw transport.
type Compatibility service

func (s *Compatibility) StartCollectorTask(ctx context.Context, networkID string) (*ports.ForwardHTTPDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	q := url.Values{"networkId": []string{networkID}, "type": []string{"NETWORK_COLLECTION"}}
	return s.document(ctx, http.MethodPost, "/api/collector-tasks?"+q.Encode(), "Compatibility.StartCollectorTask")
}

func (s *Compatibility) ListSnapshots(ctx context.Context, networkID string) (*ports.ForwardHTTPDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	return s.document(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/snapshots", "Compatibility.ListSnapshots")
}

func (s *Compatibility) Checks(ctx context.Context, snapshotID string) (*ports.ForwardHTTPDocument, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := topologySnapshotPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	return s.document(ctx, http.MethodGet, path+"/checks", "Compatibility.Checks")
}

func (s *Compatibility) document(ctx context.Context, method, path, operation string) (*ports.ForwardHTTPDocument, *ports.Response, error) {
	req, err := s.client.NewRequest(ctx, method, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	var body bytes.Buffer
	response, err := s.client.doAccepted(req, &body, true, func(int) bool { return true })
	if response == nil {
		return nil, response, err
	}
	out := &ports.ForwardHTTPDocument{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: body.Bytes()}
	return out, response, err
}
