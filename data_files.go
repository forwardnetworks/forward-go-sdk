package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

// DataFilesService manages Data Files: datasets (CSV, JSON, XML, YAML, XLSX,
// text) an org uploads so NQE queries can join them with the network model.
// A file attached to a network is collected with its snapshots and appears in
// NQE as network.extensions.<NQEName>, a record {status: OK | MISSING |
// INVALID_DATA, value?: <the file's inferred type>}. That field exists only in
// an org that has data files, so it is absent from the standard schema; GET
// /api/nqe/schema returns the org's own schema, extensions included.
// DataFileController; every route is on primary 15398425a69 and stable
// 67e89c87124. Preview: not in the published spec.
type DataFilesService service

// DataFileType is a data file's format.
type DataFileType string

const (
	DataFileCSV  DataFileType = "CSV"
	DataFileJSON DataFileType = "JSON"
	DataFileXML  DataFileType = "XML"
	DataFileXLSX DataFileType = "XLSX"
	DataFileYAML DataFileType = "YAML"
	DataFileText DataFileType = "TEXT"
	// DataFileSTIG is the STIG policy file, which has a fixed name and is in
	// every network.
	DataFileSTIG DataFileType = "STIG"
)

func (t DataFileType) valid() bool {
	switch t {
	case DataFileCSV, DataFileJSON, DataFileXML, DataFileXLSX, DataFileYAML, DataFileText, DataFileSTIG:
		return true
	}
	return false
}

// DataFile is one data file (DataFileInfo). NQEName is the field under
// network.extensions that a query reads it by. NetworkIDs are the networks it
// is attached to. IsEmpty is true for a file uploaded without content.
// Times are ISO-8601; the *By fields are usernames.
type DataFile struct {
	Name          string       `json:"name"`
	NQEName       string       `json:"nqeName"`
	Description   string       `json:"description,omitempty"`
	Type          DataFileType `json:"type"`
	NetworkIDs    []string     `json:"networkIds,omitempty"`
	IsEmpty       bool         `json:"isEmpty,omitempty"`
	ContentMD5Hex string       `json:"contentMd5Hex,omitempty"`
	CreatedAt     string       `json:"createdAt,omitempty"`
	CreatedBy     string       `json:"createdBy,omitempty"`
	CreatedByID   Identifier   `json:"createdById,omitempty"`
	UpdatedAt     string       `json:"updatedAt,omitempty"`
	UpdatedBy     string       `json:"updatedBy,omitempty"`
	UpdatedByID   Identifier   `json:"updatedById,omitempty"`
	UploadedAt    string       `json:"uploadedAt,omitempty"`
	UploadedBy    string       `json:"uploadedBy,omitempty"`
	UploadedByID  Identifier   `json:"uploadedById,omitempty"`
}

// List returns the org's data files. GET /api/data-files (VIEW_DATA_FILES).
func (s *DataFilesService) List(ctx context.Context) ([]DataFile, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/data-files", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DataFiles.List")
	var out []DataFile
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// Content writes up to maxBytes of a data file's stored content to dst. GET
// /api/data-files/{name}?view=content (VIEW_DATA_FILES). Forward refuses an
// empty file with 400 "File cannot be downloaded as it has no content.".
func (s *DataFilesService) Content(ctx context.Context, name string, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *Response, err error) {
	path, err := dataFilePath(name)
	if err != nil {
		return 0, false, nil, err
	}
	// The controller is produces=JSON, so the Accept header must admit JSON
	// for the download (and its errors) to be served at all.
	return downloadHead(ctx, s.client, path+"?view=content", "DataFiles.Content", "application/json, */*", maxBytes, dst)
}

// ListForNetwork returns the names of the data files attached to a network.
// GET /api/networks/{networkId}/data-files (VIEW_COLLECTION_SOURCES).
func (s *DataFilesService) ListForNetwork(ctx context.Context, networkID string) ([]string, *Response, error) {
	path, err := s.networkPath(networkID, "")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DataFiles.ListForNetwork")
	var out []string
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// DataFileInference is what Forward would make of a file (DataFileInference):
// Content is the first rows of a CSV, or the whole content otherwise, and
// Inference the format, diagnostics and NQE type it inferred.
type DataFileInference struct {
	Content   string                `json:"content"`
	Inference DataFileInferenceInfo `json:"inference"`
}

// DataFileInferenceInfo is an inference result (InferenceResultWithSchema).
// DataFormat is JSON, CSV or TEXT, empty when no format was recognized.
// Errors mean Forward would not accept the file as that type. Schema is the
// inferred NQE type in the same InlinedType form GET /api/nqe/schema returns,
// kept raw; it is absent when nothing was inferred.
type DataFileInferenceInfo struct {
	DataFormat string          `json:"dataFormat,omitempty"`
	Warnings   []string        `json:"warnings"`
	Errors     []string        `json:"errors"`
	Schema     json.RawMessage `json:"schema,omitempty"`
}

// InferSchema previews what Forward would make of content as fileType --
// format, NQE type and diagnostics -- without storing anything. POST
// /api/data-files?action=inferSchema, multipart file and fileType
// (MANAGE_DATA_FILES, although nothing is written). Forward refuses STIG, and
// files over 50 MB.
func (s *DataFilesService) InferSchema(ctx context.Context, fileName string, content []byte, fileType DataFileType) (*DataFileInference, *Response, error) {
	if !fileType.valid() || fileType == DataFileSTIG {
		return nil, nil, fmt.Errorf("forward: schema inference needs a non-STIG data file type, got %q", fileType)
	}
	body, contentType, err := dataFileMultipart(func(w *multipart.Writer) error {
		if err := writeDataFilePart(w, fileName, content); err != nil {
			return err
		}
		return w.WriteField("fileType", string(fileType))
	})
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/data-files?action=inferSchema", body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req = markOperation(req, "DataFiles.InferSchema")
	out := new(DataFileInference)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Schema returns what Forward infers from a stored data file, in the same
// form as InferSchema. GET /api/data-files/{name}/schema (VIEW_DATA_FILES;
// refused for the STIG policy file).
func (s *DataFilesService) Schema(ctx context.Context, name string) (*DataFileInference, *Response, error) {
	path, err := dataFilePath(name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/schema", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DataFiles.Schema")
	out := new(DataFileInference)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// DataFileCreateRequest describes a new data file. Forward lower-cases Name,
// and an empty NQEName defaults to Name without its extension. Headers, CSV
// only, names the columns of a file that has no header row.
type DataFileCreateRequest struct {
	Name        string       `json:"name"`
	NQEName     string       `json:"nqeName,omitempty"`
	Description string       `json:"description,omitempty"`
	FileType    DataFileType `json:"fileType"`
	Headers     []string     `json:"headers,omitempty"`
}

// Add uploads a new data file and returns it. It is attached to no network
// until AddToNetwork. POST /api/data-files, multipart request (JSON) and file
// (MANAGE_DATA_FILES; 201). Forward refuses files over 50 MB, headers on a
// non-CSV file, duplicate headers, and a STIG type under any name but the
// STIG policy file's.
func (s *DataFilesService) Add(ctx context.Context, request DataFileCreateRequest, content []byte) (*DataFile, *Response, error) {
	if request.Name = strings.TrimSpace(request.Name); request.Name == "" {
		return nil, nil, errors.New("forward: data file name is required")
	}
	if !request.FileType.valid() {
		return nil, nil, fmt.Errorf("forward: invalid data file type %q", request.FileType)
	}
	if len(request.Headers) != 0 && request.FileType != DataFileCSV {
		return nil, nil, errors.New("forward: data file headers apply only to CSV files")
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, nil, err
	}
	body, contentType, err := dataFileMultipart(func(w *multipart.Writer) error {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="request"`)
		header.Set("Content-Type", "application/json")
		part, err := w.CreatePart(header)
		if err != nil {
			return err
		}
		if _, err := part.Write(requestJSON); err != nil {
			return err
		}
		return writeDataFilePart(w, request.Name, content)
	})
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/data-files", body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req = markOperation(req, "DataFiles.Add")
	out := new(DataFile)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// AddToNetwork attaches a data file to a network, so its next snapshots
// collect it. POST /api/networks/{networkId}/data-files/{name}
// (MANAGE_DATA_FILES and MANAGE_COLLECTION_SOURCES; 204). The STIG policy
// file is refused: it is in every network already.
func (s *DataFilesService) AddToNetwork(ctx context.Context, networkID, name string) (*Response, error) {
	return s.attachment(ctx, http.MethodPost, networkID, name, "DataFiles.AddToNetwork")
}

// RemoveFromNetwork detaches a data file from a network; snapshots taken after
// no longer carry it, so queries reading it see status MISSING. DELETE
// /api/networks/{networkId}/data-files/{name} (DELETE_COLLECTION_SOURCES;
// 204). The STIG policy file cannot be removed from any network.
func (s *DataFilesService) RemoveFromNetwork(ctx context.Context, networkID, name string) (*Response, error) {
	return s.attachment(ctx, http.MethodDelete, networkID, name, "DataFiles.RemoveFromNetwork")
}

func (s *DataFilesService) attachment(ctx context.Context, method, networkID, name, operation string) (*Response, error) {
	if name = strings.TrimSpace(name); name == "" {
		return nil, errors.New("forward: data file name is required")
	}
	path, err := s.networkPath(networkID, "/"+url.PathEscape(name))
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, method, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	return s.client.Do(req, nil)
}

func (s *DataFilesService) networkPath(networkID, tail string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/data-files" + tail, nil
}

func dataFilePath(name string) (string, error) {
	if name = strings.TrimSpace(name); name == "" {
		return "", errors.New("forward: data file name is required")
	}
	return "/api/data-files/" + url.PathEscape(name), nil
}

// dataFileMultipart buffers a multipart body: Forward caps data files at
// 50 MB, so streaming is not worth a goroutine.
func dataFileMultipart(write func(*multipart.Writer) error) (*bytes.Buffer, string, error) {
	body := new(bytes.Buffer)
	w := multipart.NewWriter(body)
	if err := write(w); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return body, w.FormDataContentType(), nil
}

func writeDataFilePart(w *multipart.Writer, fileName string, content []byte) error {
	if fileName = strings.TrimSpace(fileName); fileName == "" {
		fileName = "data"
	}
	part, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return err
	}
	_, err = part.Write(content)
	return err
}
