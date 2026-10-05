package forward

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Assess is a POST with action=assess and a bare JSON array; Forward answers {"approved":[...],"unapproved":[...]}. An
// empty input is answered locally, because Forward would partition nothing and the caller needs no request for that.
func TestAssessCLICommands(t *testing.T) {
	t.Parallel()

	var path string
	var sent []string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		path = r.Method + " " + r.URL.RequestURI()
		_ = json.NewDecoder(r.Body).Decode(&sent)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"approved":["show version"],"unapproved":["reload"]}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)

	got, _, err := c.Endpoints.AssessCLICommands(context.Background(), []string{"show version", "reload"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "POST /api/approved-cli-commands?action=assess" || len(sent) != 2 {
		t.Errorf("request = %s %v", path, sent)
	}
	if len(got.Approved) != 1 || got.Approved[0] != "show version" || got.Unapproved[0] != "reload" {
		t.Errorf("assessment = %+v", got)
	}
	empty, _, err := c.Endpoints.AssessCLICommands(context.Background(), nil)
	if err != nil || calls != 1 || empty.Approved == nil || empty.Unapproved == nil {
		t.Errorf("empty input: %+v, calls=%d, err=%v", empty, calls, err)
	}
}

// Update sends the signed file untouched as the multipart part "file"; the SDK cannot sign and must not alter it.
func TestUpdateApprovedCLICommandsSendsTheFileUntouched(t *testing.T) {
	t.Parallel()

	var path, name string
	var content []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.RequestURI()
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		part, err := multipart.NewReader(r.Body, params["boundary"]).NextPart()
		if err != nil {
			t.Error(err)
			return
		}
		name = part.FormName()
		content, _ = io.ReadAll(part)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"commands":["^show "],"signedAt":"2026-10-05T00:00:00Z"}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)

	signed := []byte(`{"commands":["^show "],"signature":"abc"}`)
	got, _, err := c.Endpoints.UpdateApprovedCLICommands(context.Background(), "cmds.json", signed)
	if err != nil {
		t.Fatal(err)
	}
	if path != "POST /api/approved-cli-commands?action=update" || name != "file" || string(content) != string(signed) {
		t.Errorf("request = %s part=%q content=%q", path, name, content)
	}
	if len(got.Commands) != 1 || got.SignedAt == "" {
		t.Errorf("result = %+v", got)
	}
	if _, _, err := c.Endpoints.UpdateApprovedCLICommands(context.Background(), "x", nil); err == nil {
		t.Error("an empty file must be refused before any request")
	}
}
