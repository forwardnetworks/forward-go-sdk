package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// DataFileInfo unwraps Attribution (createdById/At/By, ...) and the upload
// ActionAttribution under the prefix "uploaded"; isEmpty is only sent when
// true. The network list is a bare array of names.
func TestDataFilesReads(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/api/data-files":
			_, _ = io.WriteString(w, `[{"name":"sites.csv","nqeName":"sites","description":"site owners","type":"CSV",
			  "createdById":"12","createdAt":"2026-10-01T10:00:00Z","createdBy":"mary","updatedById":"12","updatedAt":"2026-10-01T10:00:00Z",
			  "uploadedById":"12","uploadedAt":"2026-10-01T10:00:00Z","uploadedBy":"mary","networkIds":["3347"],"contentMd5Hex":"abc"},
			  {"name":"later.json","nqeName":"later","type":"JSON","isEmpty":true}]`)
		case "/api/networks/3347/data-files":
			_, _ = io.WriteString(w, `["sites.csv"]`)
		case "/api/data-files/sites.csv?view=content":
			if !strings.Contains(r.Header.Get("Accept"), "application/json") {
				w.WriteHeader(http.StatusNotAcceptable) // the controller is produces=JSON
				return
			}
			w.Header().Set("Content-Type", "text/csv")
			_, _ = io.WriteString(w, "site,owner\nsjc,mary\n")
		case "/api/data-files/sites.csv/schema":
			_, _ = io.WriteString(w, `{"content":"site,owner\nsjc,mary\n","inference":{"dataFormat":"CSV","warnings":[],"errors":[],"schema":{"kind":"list"}}}`)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	files := newTestClient(t, server.URL).DataFiles
	ctx := context.Background()

	list, _, err := files.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("List() = %+v, %v", list, err)
	}
	if f := list[0]; f.NQEName != "sites" || f.Type != DataFileCSV || f.NetworkIDs[0] != "3347" || f.UploadedBy != "mary" || f.CreatedByID != "12" || f.IsEmpty {
		t.Fatalf("data file = %+v", f)
	}
	if !list[1].IsEmpty {
		t.Fatal("isEmpty:true must decode")
	}
	names, _, err := files.ListForNetwork(ctx, "3347")
	if err != nil || len(names) != 1 || names[0] != "sites.csv" {
		t.Fatalf("ListForNetwork() = %v, %v", names, err)
	}
	var content bytes.Buffer
	n, truncated, _, err := files.Content(ctx, "sites.csv", 10, &content)
	if err != nil || n != 10 || !truncated || content.String() != "site,owner" {
		t.Fatalf("Content() = %d %v %q %v", n, truncated, content.String(), err)
	}
	schema, _, err := files.Schema(ctx, "sites.csv")
	if err != nil || schema.Inference.DataFormat != "CSV" || string(schema.Inference.Schema) != `{"kind":"list"}` {
		t.Fatalf("Schema() = %+v, %v", schema, err)
	}
}

// Add sends Forward's two @RequestParts: "request" as JSON (Spring needs the
// part's Content-Type to bind it) and "file"; inferSchema sends a file and a
// plain fileType field and stores nothing.
func TestDataFilesUploads(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Errorf("multipart: %v", err)
			return
		}
		parts := map[string][]byte{}
		headers := map[string]string{}
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(part)
			parts[part.FormName()] = data
			headers[part.FormName()] = part.Header.Get("Content-Type") + "|" + part.FileName()
		}
		switch r.URL.RequestURI() {
		case "/api/data-files?action=inferSchema":
			if string(parts["fileType"]) != "JSON" || string(parts["file"]) != `{"a":1}` || !strings.HasSuffix(headers["file"], "|x.json") {
				t.Errorf("inferSchema parts: %q %q", parts, headers)
			}
			_, _ = io.WriteString(w, `{"content":"{\"a\":1}","inference":{"dataFormat":"JSON","warnings":["w"],"errors":[]}}`)
		case "/api/data-files":
			if headers["request"] != "application/json|" {
				t.Errorf("request part headers = %q; Spring binds a @RequestPart object only from a JSON-typed part", headers["request"])
			}
			var got map[string]any
			_ = json.Unmarshal(parts["request"], &got)
			if got["name"] != "sites.csv" || got["fileType"] != "CSV" || got["nqeName"] != nil || string(parts["file"]) != "site\nsjc\n" {
				t.Errorf("add parts: request=%s file=%q", parts["request"], parts["file"])
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"name":"sites.csv","nqeName":"sites","type":"CSV","networkIds":[]}`)
		}
	}))
	defer server.Close()
	files := newTestClient(t, server.URL).DataFiles
	ctx := context.Background()

	inf, _, err := files.InferSchema(ctx, "x.json", []byte(`{"a":1}`), DataFileJSON)
	if err != nil || inf.Inference.DataFormat != "JSON" || inf.Inference.Warnings[0] != "w" || inf.Inference.Schema != nil {
		t.Fatalf("InferSchema() = %+v, %v", inf, err)
	}
	if _, _, err := files.InferSchema(ctx, "p.csv", nil, DataFileSTIG); err == nil {
		t.Fatal("STIG inference must be refused locally")
	}
	added, _, err := files.Add(ctx, DataFileCreateRequest{Name: " sites.csv ", FileType: DataFileCSV}, []byte("site\nsjc\n"))
	if err != nil || added.NQEName != "sites" {
		t.Fatalf("Add() = %+v, %v", added, err)
	}
	if _, _, err := files.Add(ctx, DataFileCreateRequest{Name: "a.json", FileType: DataFileJSON, Headers: []string{"x"}}, nil); err == nil {
		t.Fatal("headers on a non-CSV file must be refused locally")
	}
}

func TestDataFilesNetworkAttachment(t *testing.T) {
	t.Parallel()

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		if strings.HasSuffix(r.URL.Path, "/stig_policy.csv") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"Stig policy file can not be excluded from any network."}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	files := newTestClient(t, server.URL).DataFiles
	ctx := context.Background()

	if _, err := files.AddToNetwork(ctx, "3347", "sites.csv"); err != nil || calls[0] != "POST /api/networks/3347/data-files/sites.csv" {
		t.Fatalf("AddToNetwork: %v %v", err, calls)
	}
	if _, err := files.RemoveFromNetwork(ctx, "3347", "a b.csv"); err != nil || calls[1] != "DELETE /api/networks/3347/data-files/a%20b.csv" {
		t.Fatalf("RemoveFromNetwork: %v %v", err, calls)
	}
	if _, err := files.RemoveFromNetwork(ctx, "3347", "stig_policy.csv"); !IsStatus(err, http.StatusBadRequest) {
		t.Fatalf("the STIG refusal must surface: %v", err)
	}
	if _, err := files.AddToNetwork(ctx, "3347", " "); err == nil {
		t.Fatal("an empty name must be refused")
	}
}
