package rest

import (
	"bytes"
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// ServiceNow change requests raised for a Predict change set (ServiceNowController, master fe0daf63c899). Preview: not in the published
// spec. Create and attach need the organization properties PREDICT_MODELING and SERVICENOW_CHANGE_REQUESTS (default off, not org-admin
// configurable), the deployment's outbound connections, the ServiceNow integration settings (Integrations.GetServiceNow), and
// MANAGE_CHANGE_SETS on the network; reads and downloads need only the permission and the outbound connections.

func (s *Integrations) changeRequestPath(networkID, changeSetID string, rest ...string) (string, error) {
	changeSetID = strings.TrimSpace(changeSetID)
	if changeSetID == "" {
		return "", errors.New("forward: change-set ID is required")
	}
	parts := []string{"change-sets", url.PathEscape(changeSetID), "servicenow", "change-requests"}
	for _, part := range rest {
		if part = strings.TrimSpace(part); part == "" {
			return "", errors.New("forward: change request and attachment IDs are required")
		}
		parts = append(parts, url.PathEscape(part))
	}
	return s.client.networkSubPath(networkID, parts...)
}

// CreateChangeSetChangeRequest raises a ServiceNow change request for a change set (POST .../change-sets/{id}/servicenow/change-requests).
// Forward checks the change set exists before it creates the ticket and creates the ticket before it links it. A change set's linked requests
// (number and sys id) are listed by Predict.GetChangeSetSummary, so a lost response is recoverable only if the link was made; keep the answer's
// SysID anyway.
func (s *Integrations) CreateChangeSetChangeRequest(ctx context.Context, networkID, changeSetID string, input ports.ServiceNowChangeRequestInput) (*ports.ServiceNowChangeRequest, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(input.Summary) == "" {
		return nil, nil, errors.New("forward: a change request needs a summary")
	}
	switch input.Impact {
	case ports.ServiceNowImpactHigh, ports.ServiceNowImpactMedium, ports.ServiceNowImpactLow:
	default:
		return nil, nil, errors.New("forward: a change request's impact is HIGH, MEDIUM or LOW")
	}
	switch input.Priority {
	case "", ports.ServiceNowPriorityCritical, ports.ServiceNowPriorityHigh, ports.ServiceNowPriorityModerate, ports.ServiceNowPriorityLow:
	default:
		return nil, nil, errors.New("forward: a change request's priority is CRITICAL, HIGH, MODERATE or LOW")
	}
	path, err := s.changeRequestPath(networkID, changeSetID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.ServiceNowChangeRequest)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.CreateChangeSetChangeRequest"), out)
	return out, resp, err
}

// GetChangeSetChangeRequest reads a change request of a change set by its ServiceNow sys id (GET .../change-requests/{sysId}).
func (s *Integrations) GetChangeSetChangeRequest(ctx context.Context, networkID, changeSetID, sysID string) (*ports.ServiceNowChangeRequest, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.changeRequestPath(networkID, changeSetID, sysID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.ServiceNowChangeRequest)
	resp, err := s.client.doRequired(markOperation(req, "Integrations.GetChangeSetChangeRequest"), out)
	return out, resp, err
}

// AttachToChangeSetChangeRequest uploads a file to a change request (POST .../change-requests/{sysId}/attachments, multipart part "file";
// 204). Forward refuses more than ServiceNowMaxAttachmentBytes (this SDK refuses it first) and a file type outside png, jpg, jpeg, pdf,
// xlsx, docx and csv (400). It shortens a file name past ServiceNowMaxAttachmentFileName characters rather than refusing it.
func (s *Integrations) AttachToChangeSetChangeRequest(ctx context.Context, networkID, changeSetID, sysID, fileName string, content []byte) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if fileName = strings.TrimSpace(fileName); fileName == "" {
		return nil, errors.New("forward: an attachment needs a file name")
	}
	if len(content) == 0 || len(content) > ports.ServiceNowMaxAttachmentBytes {
		return nil, errors.New("forward: an attachment is 1 byte to 25 MB")
	}
	path, err := s.changeRequestPath(networkID, changeSetID, sysID)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(content); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/attachments", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return s.client.Do(markOperation(req, "Integrations.AttachToChangeSetChangeRequest"), nil)
}

// DownloadChangeSetChangeRequestAttachment writes an attachment of a change request to w (GET .../change-requests/{sysId}/attachments/{fileSysId}).
// Forward audits the download and sends the headers before the bytes, so a failure mid-stream leaves partial bytes in w with the error.
func (s *Integrations) DownloadChangeSetChangeRequestAttachment(ctx context.Context, networkID, changeSetID, sysID, fileSysID string, w io.Writer) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if w == nil {
		return nil, errors.New("forward: a writer is required")
	}
	path, err := s.changeRequestPath(networkID, changeSetID, sysID, "attachments", fileSysID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream, application/json")
	return s.client.Do(markOperation(req, "Integrations.DownloadChangeSetChangeRequestAttachment"), w)
}
