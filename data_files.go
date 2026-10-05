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

// Delete removes a data file from the organization's library, and so from
// EVERY network it was attached to; queries reading it then see no
// network.extensions.<nqeName> field for it. RemoveFromNetwork only detaches
// one network. A file that does not exist counts as success (Forward answers
// 404 "No data file with name '...' exists."). DELETE /api/data-files/{name}
// (MANAGE_DATA_FILES). The content is not kept anywhere else: download it
// first with Content if it may be needed again.
func (s *DataFilesService) Delete(ctx context.Context, name string) (*Response, error) {
	path, err := dataFilePath(name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "DataFiles.Delete")
	resp, err := s.client.Do(req, nil)
	if isGone(err) {
		return resp, nil
	}
	return resp, err
}

// DataFilePatch changes a data file's details, not its content. Nil fields
// are left alone. Name renames the file (Forward lower-cases it and refuses a
// name a collection source already uses in any network the file is attached
// to); NQEName changes the field queries read it by, network.extensions.<name>,
// and may not be blank; Description Ptr("") clears it. At least one field is
// required. Forward refuses to patch the STIG policy file.
type DataFilePatch struct {
	Name        *string
	NQEName     *string
	Description *string
}

// MarshalJSON writes only the stated fields; clearing the description is an
// explicit null (DataFilePatch.description is a JsonProp).
func (p DataFilePatch) MarshalJSON() ([]byte, error) {
	body := map[string]any{}
	if p.Name != nil {
		body["name"] = *p.Name
	}
	if p.NQEName != nil {
		body["nqeName"] = *p.NQEName
	}
	if p.Description != nil {
		if *p.Description == "" {
			body["description"] = nil
		} else {
			body["description"] = *p.Description
		}
	}
	return json.Marshal(body)
}

// Patch changes a data file's name, NQE name or description and returns it.
// The content is untouched. PATCH /api/data-files/{name} (MANAGE_DATA_FILES;
// 404 if the file does not exist). Renaming or changing NQEName changes what
// queries must call it, so it is a breaking change for any query reading the
// old name.
func (s *DataFilesService) Patch(ctx context.Context, name string, patch DataFilePatch) (*DataFile, *Response, error) {
	if patch.Name == nil && patch.NQEName == nil && patch.Description == nil {
		return nil, nil, errors.New("forward: a data file patch must change something")
	}
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return nil, nil, errors.New("forward: a data file cannot be renamed to nothing")
	}
	if patch.NQEName != nil && strings.TrimSpace(*patch.NQEName) == "" {
		return nil, nil, errors.New("forward: a data file's NQE name may not be blank")
	}
	path, err := dataFilePath(name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DataFiles.Patch")
	out := new(DataFile)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// ReplaceContent replaces the content of an existing data file and returns
// it. The file keeps its name, type, NQE name and network attachments; headers
// name the columns of a CSV that has no header row, and are sent like Add's.
// POST /api/data-files/{name}, multipart file and headerRow (MANAGE_DATA_FILES;
// 404 if the file does not exist; 50 MB cap).
//
// Forward checks that the new content parses as the file's type and infers
// its NQE type, but does NOT check that queries which read the file still
// work: incompatible content is accepted. Run AssessReplacement first.
//
// To skip a replace that would change nothing, compare DataFile.ContentMD5Hex
// (List) with the MD5 of the bytes Forward would store: your content as sent,
// except that a CSV sent with headers is stored with the header line added in
// front (I did not verify that format), and I did not verify the hash's letter
// case, so compare case-insensitively.
func (s *DataFilesService) ReplaceContent(ctx context.Context, name string, content []byte, headers []string) (*DataFile, *Response, error) {
	req, err := s.replaceRequest(ctx, name, content, headers, "", "DataFiles.ReplaceContent")
	if err != nil {
		return nil, nil, err
	}
	out := new(DataFile)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// AssessReplacement reports whether content could replace a data file without
// breaking the queries that read it: true when the new content's inferred NQE
// type is a subtype of the stored content's, so any query that works today
// still works. It changes nothing. False also covers content that does not
// parse as the file's type, which is reported as incompatible, not as an
// error. POST /api/data-files/{name}?action=assess (MANAGE_DATA_FILES; refused
// for the STIG policy file; 404 if the file does not exist).
func (s *DataFilesService) AssessReplacement(ctx context.Context, name string, content []byte, headers []string) (bool, *Response, error) {
	req, err := s.replaceRequest(ctx, name, content, headers, "assess", "DataFiles.AssessReplacement")
	if err != nil {
		return false, nil, err
	}
	var out struct {
		Result string `json:"result"`
	}
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return false, resp, err
	}
	switch out.Result {
	case "SUCCESS":
		return true, resp, nil
	case "FAILURE":
		return false, resp, nil
	}
	return false, resp, fmt.Errorf("forward: unexpected data file assessment result %q", out.Result)
}

func (s *DataFilesService) replaceRequest(ctx context.Context, name string, content []byte, headers []string, action, operation string) (*http.Request, error) {
	path, err := dataFilePath(name)
	if err != nil {
		return nil, err
	}
	if action != "" {
		path += "?" + url.Values{"action": []string{action}}.Encode()
	}
	body, contentType, err := dataFileMultipart(func(w *multipart.Writer) error {
		if err := writeDataFilePart(w, name, content); err != nil {
			return err
		}
		if len(headers) == 0 {
			return nil
		}
		headerJSON, err := json.Marshal(headers)
		if err != nil {
			return err
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="headerRow"`)
		header.Set("Content-Type", "application/json")
		part, err := w.CreatePart(header)
		if err != nil {
			return err
		}
		_, err = part.Write(headerJSON)
		return err
	})
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return markOperation(req, operation), nil
}
