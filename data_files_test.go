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

// Replace keeps the file and sends "file" plus, only when headers are given, a
// JSON-typed "headerRow" part; assess is the same request with ?action=assess
// and answers SUCCESS or FAILURE in a 200.
func TestDataFilesReplaceAndAssess(t *testing.T) {
	t.Parallel()

	type seen struct{ method, uri, file, header, headerType string }
	var calls []seen
	result := "SUCCESS"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Errorf("multipart: %v", err)
			return
		}
		c := seen{method: r.Method, uri: r.URL.RequestURI()}
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(part)
			switch part.FormName() {
			case "file":
				c.file = string(data)
			case "headerRow":
				c.header, c.headerType = string(data), part.Header.Get("Content-Type")
			}
		}
		calls = append(calls, c)
		if r.URL.Query().Get("action") == "assess" {
			_, _ = io.WriteString(w, `{"result":"`+result+`"}`)
			return
		}
		if r.URL.Path == "/api/data-files/missing.csv" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"name":"sites.csv","nqeName":"sites","type":"CSV","contentMd5Hex":"abc","networkIds":["3347"]}`)
	}))
	defer server.Close()
	files := newTestClient(t, server.URL).DataFiles
	ctx := context.Background()

	got, _, err := files.ReplaceContent(ctx, "sites.csv", []byte("sjc,mary\n"), []string{"site", "owner"})
	if err != nil || got.ContentMD5Hex != "abc" || got.NetworkIDs[0] != "3347" {
		t.Fatalf("ReplaceContent() = %+v, %v", got, err)
	}
	if c := calls[0]; c.method != http.MethodPost || c.uri != "/api/data-files/sites.csv" || c.file != "sjc,mary\n" || c.header != `["site","owner"]` || c.headerType != "application/json" {
		t.Fatalf("replace sent %+v", c)
	}
	if _, _, err := files.ReplaceContent(ctx, "sites.csv", []byte("x\n"), nil); err != nil || calls[1].header != "" {
		t.Fatalf("no headers must send no headerRow part: %v %+v", err, calls[1])
	}
	ok, _, err := files.AssessReplacement(ctx, "sites.csv", []byte("x\n"), nil)
	if err != nil || !ok || calls[2].uri != "/api/data-files/sites.csv?action=assess" {
		t.Fatalf("assess SUCCESS = %v, %v; %+v", ok, err, calls[2])
	}
	result = "FAILURE"
	if ok, _, err := files.AssessReplacement(ctx, "sites.csv", []byte("x\n"), nil); err != nil || ok {
		t.Fatalf("assess FAILURE = %v, %v", ok, err)
	}
	result = "MAYBE"
	if _, _, err := files.AssessReplacement(ctx, "sites.csv", []byte("x\n"), nil); err == nil {
		t.Fatal("an unknown assessment result must be an error, not a pass")
	}
	if _, _, err := files.ReplaceContent(ctx, "missing.csv", []byte("x"), nil); !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("a missing file must surface its 404: %v", err)
	}
}

func TestDataFilesPatch(t *testing.T) {
	t.Parallel()

	var method, uri, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, uri, body = r.Method, r.URL.RequestURI(), string(b)
		_, _ = io.WriteString(w, `{"name":"owners.csv","nqeName":"owners","type":"CSV"}`)
	}))
	defer server.Close()
	files := newTestClient(t, server.URL).DataFiles
	ctx := context.Background()

	got, _, err := files.Patch(ctx, "sites.csv", DataFilePatch{Name: Ptr("owners.csv"), NQEName: Ptr("owners")})
	if err != nil || got.Name != "owners.csv" || method != http.MethodPatch || uri != "/api/data-files/sites.csv" || body != `{"name":"owners.csv","nqeName":"owners"}` {
		t.Fatalf("rename: %+v %v %s %s %s", got, err, method, uri, body)
	}
	if _, _, err := files.Patch(ctx, "sites.csv", DataFilePatch{Description: Ptr("")}); err != nil || body != `{"description":null}` {
		t.Fatalf("an empty description must clear with an explicit null: %v %s", err, body)
	}
	if _, _, err := files.Patch(ctx, "sites.csv", DataFilePatch{Description: Ptr("site owners")}); err != nil || body != `{"description":"site owners"}` {
		t.Fatalf("set description: %v %s", err, body)
	}
	before := body
	for name, patch := range map[string]DataFilePatch{
		"empty":          {},
		"blank NQE name": {NQEName: Ptr(" ")},
		"blank new name": {Name: Ptr("")},
	} {
		if _, _, err := files.Patch(ctx, "sites.csv", patch); err == nil || body != before {
			t.Errorf("%s patch must be refused locally", name)
		}
	}
}
