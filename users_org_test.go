package forward

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordedRequest struct {
	Method, Path, Query, ContentType string
	Body                             []byte
}

func recordingClient(t *testing.T, status int, body string, headers map[string]string) (*Client, *[]recordedRequest) {
	t.Helper()
	var seen []recordedRequest
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, recordedRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, ContentType: r.Header.Get("Content-Type"), Body: b})
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		if body != "" && headers["Content-Type"] == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	return c, &seen
}

func TestUsersCreatePostsToOrgAdminRouteAndReturnsID(t *testing.T) {
	c, seen := recordingClient(t, http.StatusOK, `{"id":"2701","username":"seat-07@autocon6.invalid","email":"seat-07@autocon6.invalid","enabled":true}`, nil)
	user, _, err := c.Users.Create(context.Background(), UserCreateRequest{Email: " seat-07@autocon6.invalid ", Password: "pw"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if user.ID != "2701" {
		t.Fatalf("user ID = %q, want 2701", user.ID)
	}
	got := (*seen)[0]
	if got.Method != http.MethodPost || got.Path != "/api/users" {
		t.Fatalf("request = %s %s, want POST /api/users", got.Method, got.Path)
	}
	var body map[string]any
	if err := json.Unmarshal(got.Body, &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body["email"] != "seat-07@autocon6.invalid" || body["password"] != "pw" {
		t.Fatalf("body = %v", body)
	}
	if _, ok := body["username"]; ok {
		t.Fatalf("empty username must be omitted so the server defaults it to the email: %v", body)
	}
	if strings.Contains(got.Query, "pw") {
		t.Fatal("password leaked into the query string")
	}
}

func TestUsersCreateRequiresAnIDInTheResponse(t *testing.T) {
	c, _ := recordingClient(t, http.StatusOK, `{"username":"x"}`, nil)
	if _, _, err := c.Users.Create(context.Background(), UserCreateRequest{Email: "x@y", Password: "pw"}); err == nil {
		t.Fatal("Create accepted a response without a user ID")
	}
}

func TestUsersCreateValidatesBeforeSending(t *testing.T) {
	c, seen := recordingClient(t, http.StatusOK, `{}`, nil)
	if _, _, err := c.Users.Create(context.Background(), UserCreateRequest{Email: "x@y"}); err == nil {
		t.Fatal("Create accepted an empty password")
	}
	if len(*seen) != 0 {
		t.Fatal("Create sent a request it should have refused")
	}
}

func TestUsersListReadsTheUsersEnvelope(t *testing.T) {
	c, seen := recordingClient(t, http.StatusOK, `{"users":[{"id":"1","username":"a"},{"id":"2","username":"b"}]}`, nil)
	users, _, err := c.Users.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 2 || users[1].ID != "2" {
		t.Fatalf("users = %+v", users)
	}
	if got := (*seen)[0]; got.Method != http.MethodGet || got.Path != "/api/users" {
		t.Fatalf("request = %s %s", got.Method, got.Path)
	}
}

func TestNetworkRoleRoutes(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name        string
		call        func(*Client) (*Response, error)
		method      string
		path, query string
	}{
		{"set", func(c *Client) (*Response, error) { return c.Users.SetNetworkRole(ctx, "7", "42", NetworkRoleAdmin) }, http.MethodPost, "/api/users/7/roles/network/42", "role=ADMIN"},
		{"add", func(c *Client) (*Response, error) { return c.Users.AddNetworkRole(ctx, "7", "42", NetworkRoleOperator) }, http.MethodPost, "/api/users/7/roles/network/42/OPERATOR", ""},
		{"remove", func(c *Client) (*Response, error) {
			return c.Users.RemoveNetworkRole(ctx, "7", "42", NetworkRoleReadOnly)
		}, http.MethodDelete, "/api/users/7/roles/network/42/READ_ONLY", ""},
		{"clear", func(c *Client) (*Response, error) { return c.Users.ClearNetworkRoles(ctx, "7", "42") }, http.MethodDelete, "/api/users/7/roles/network/42", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, seen := recordingClient(t, http.StatusNoContent, "", nil)
			if _, err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			got := (*seen)[0]
			if got.Method != tc.method || got.Path != tc.path || got.Query != tc.query {
				t.Fatalf("request = %s %s?%s, want %s %s?%s", got.Method, got.Path, got.Query, tc.method, tc.path, tc.query)
			}
		})
	}
}

func TestNetworkRoleGrantInsistsOn204(t *testing.T) {
	c, _ := recordingClient(t, http.StatusOK, `{}`, nil)
	if _, err := c.Users.SetNetworkRole(context.Background(), "7", "42", NetworkRoleAdmin); err == nil {
		t.Fatal("a 200 from the role grant was accepted as success")
	}
}

func TestNetworkRoleRejectsUnknownRolesWithoutSending(t *testing.T) {
	c, seen := recordingClient(t, http.StatusNoContent, "", nil)
	if _, err := c.Users.SetNetworkRole(context.Background(), "7", "42", NetworkRole("OWNER")); err == nil {
		t.Fatal("an unknown role was accepted")
	}
	if _, err := c.Users.SetNetworkRole(context.Background(), "7", " ", NetworkRoleAdmin); err == nil {
		t.Fatal("an empty network ID was accepted")
	}
	if len(*seen) != 0 {
		t.Fatal("a refused grant still reached the server")
	}
}

func TestUserRolesDecodesTheSingleRoleShapeForwardSends(t *testing.T) {
	var roles UserRoles
	if err := json.Unmarshal([]byte(`{"org":[],"network":{"42":"ADMIN","43":["READ_ONLY"]}}`), &roles); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := roles.NetworkRoles("42"); len(got) != 1 || got[0] != "ADMIN" {
		t.Fatalf("network 42 roles = %v, want [ADMIN]", got)
	}
	if got := roles.NetworkRoles("43"); len(got) != 1 || got[0] != "READ_ONLY" {
		t.Fatalf("network 43 roles = %v, want [READ_ONLY]", got)
	}
	if roles.HasOrgAdmin() {
		t.Fatal("empty org roles read as org admin")
	}
}

func TestUserRolesKeepsAnAbsentNetworkMapNil(t *testing.T) {
	var roles UserRoles
	if err := json.Unmarshal([]byte(`{"org":["ADMIN"]}`), &roles); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if roles.Network != nil || !roles.HasOrgAdmin() {
		t.Fatalf("roles = %+v", roles)
	}
}

func TestTokensForAnotherUser(t *testing.T) {
	c, seen := recordingClient(t, http.StatusOK, `{"tokens":[{"name":"autocon6-seat-07-v1","accessKey":"abcd-efghi-jklm"}]}`, nil)
	tokens, _, err := c.Users.ListTokensFor(context.Background(), "7")
	if err != nil {
		t.Fatalf("ListTokensFor: %v", err)
	}
	if len(tokens) != 1 || tokens[0].AccessKey != "abcd-efghi-jklm" {
		t.Fatalf("tokens = %+v", tokens)
	}
	if got := (*seen)[0]; got.Path != "/api/users/7/tokens" {
		t.Fatalf("path = %s", got.Path)
	}

	gone, seenDelete := recordingClient(t, http.StatusNotFound, `{"message":"no such token"}`, nil)
	if _, err := gone.Users.DeleteTokenFor(context.Background(), "7", "autocon6-seat-07-v1"); err != nil {
		t.Fatalf("deleting an already-deleted token must succeed: %v", err)
	}
	if got := (*seenDelete)[0]; got.Method != http.MethodDelete || got.Path != "/api/users/7/tokens/autocon6-seat-07-v1" {
		t.Fatalf("request = %s %s", got.Method, got.Path)
	}
}

func TestDownloadClientPackageStreamsAndDigests(t *testing.T) {
	payload := bytes.Repeat([]byte("fwd-headless"), 4096)
	sum := sha256.Sum256(payload)
	c, seen := recordingClient(t, http.StatusOK, string(payload), map[string]string{
		"Content-Type":        "application/gzip",
		"Content-Disposition": `attachment; filename="fwd-unix-26.9.0-18.tar.gz"`,
	})
	var buf bytes.Buffer
	pkg, _, err := c.SoftwareCentral.DownloadClientPackage(context.Background(), ClientPackageHeadlessLinux, &buf)
	if err != nil {
		t.Fatalf("DownloadClientPackage: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Fatal("downloaded bytes differ from what the server sent")
	}
	if pkg.SHA256 != hex.EncodeToString(sum[:]) || pkg.Size != int64(len(payload)) || pkg.FileName != "fwd-unix-26.9.0-18.tar.gz" {
		t.Fatalf("package = %+v", pkg)
	}
	if got := (*seen)[0]; got.Path != "/api/software/client" || got.Query != "type=HEADLESS_LINUX" {
		t.Fatalf("request = %s?%s", got.Path, got.Query)
	}
}

func TestDownloadClientPackageWritesNothingOnError(t *testing.T) {
	c, _ := recordingClient(t, http.StatusBadRequest, `{"message":"Unsupported package type: HEADLESS_LINUX"}`, nil)
	var buf bytes.Buffer
	if _, _, err := c.SoftwareCentral.DownloadClientPackage(context.Background(), ClientPackageHeadlessLinux, &buf); err == nil {
		t.Fatal("a 400 was reported as a download")
	}
	if buf.Len() != 0 {
		t.Fatalf("an error body was written to the destination: %q", buf.String())
	}
}
