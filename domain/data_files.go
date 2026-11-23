package domain

import (
	"encoding/json"
)

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

func (t DataFileType) Valid() bool {
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
