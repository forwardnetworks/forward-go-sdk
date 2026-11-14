package forward

import (
	"context"
	"net/http"
	"testing"
)

// The org-level HTTP credential store is a different route family from the
// network-level one: /api/http-credentials with no network in the path. Each
// call must send exactly the verb, path and body CredentialController maps,
// and input Forward refuses (a LOGIN credential with no username, an API_KEY
// carrying a username) is refused before sending.
func TestOrgHTTPCredentialRoutes(t *testing.T) {
	t.Parallel()

	stored := `{"id":"H-3","type":"LOGIN","name":"tvm","username":"AK","password":"S-9","autoAssociate":false,"createdById":"u1","createdBy":"ada","createdAt":"2026-11-14T10:00:00Z"}`
	server, seen := vulnRecorder(t, http.StatusOK, stored)
	c := newTestClient(t, server.URL).Credentials
	ctx := context.Background()

	created, _, err := c.CreateOrgHTTP(ctx, HTTPCredentialRequest{Name: "tvm", Username: "AK", Password: "SK"})
	if err != nil || created.ID != "H-3" || created.PasswordID != "S-9" || created.CreatedBy != "ada" {
		t.Fatalf("CreateOrgHTTP = %+v, %v", created, err)
	}
	if got := (*seen)[0]; got.method != http.MethodPost || got.uri != "/api/http-credentials" || got.body != `{"name":"tvm","username":"AK","password":"SK"}` {
		t.Fatalf("create sent %+v", got)
	}

	got, _, err := c.GetOrgHTTP(ctx, "H-3")
	if err != nil || got.Name != "tvm" {
		t.Fatalf("GetOrgHTTP = %+v, %v", got, err)
	}
	if g := (*seen)[1]; g.method != http.MethodGet || g.uri != "/api/http-credentials/H-3" {
		t.Fatalf("get sent %+v", g)
	}

	newName := "tvm2"
	updated, _, err := c.UpdateOrgHTTP(ctx, "H-3", HTTPCredentialPatch{Name: &newName})
	if err != nil || updated.ID != "H-3" {
		t.Fatalf("UpdateOrgHTTP = %+v, %v", updated, err)
	}
	if g := (*seen)[2]; g.method != http.MethodPatch || g.uri != "/api/http-credentials/H-3" || g.body != `{"name":"tvm2"}` {
		t.Fatalf("update sent %+v", g)
	}

	// An API_KEY credential carries no username: it must reach the server and send no username key.
	if _, _, err := c.CreateOrgHTTP(ctx, HTTPCredentialRequest{Type: "API_KEY", Name: "key", Password: "SK"}); err != nil {
		t.Fatalf("a valid API_KEY credential was refused: %v", err)
	}
	if g := (*seen)[3]; g.body != `{"type":"API_KEY","name":"key","password":"SK"}` {
		t.Fatalf("api key create sent %s", g.body)
	}

	before := len(*seen)
	for name, call := range map[string]func() error{
		"login without username": func() error {
			_, _, err := c.CreateOrgHTTP(ctx, HTTPCredentialRequest{Name: "x", Password: "p"})
			return err
		},
		"username with space": func() error {
			_, _, err := c.CreateOrgHTTP(ctx, HTTPCredentialRequest{Name: "x", Username: "a b", Password: "p"})
			return err
		},
		"api key with username": func() error {
			_, _, err := c.CreateOrgHTTP(ctx, HTTPCredentialRequest{Type: "API_KEY", Name: "x", Username: "u", Password: "p"})
			return err
		},
		"no password": func() error {
			_, _, err := c.CreateOrgHTTP(ctx, HTTPCredentialRequest{Name: "x", Username: "u"})
			return err
		},
		"get no id": func() error { _, _, err := c.GetOrgHTTP(ctx, " "); return err },
		"update no id": func() error {
			_, _, err := c.UpdateOrgHTTP(ctx, "", HTTPCredentialPatch{})
			return err
		},
		"delete no id": func() error { _, err := c.DeleteOrgHTTP(ctx, ""); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(*seen) != before {
		t.Fatalf("a refused call reached the server: %d new requests", len(*seen)-before)
	}
}

func TestOrgHTTPCredentialListAndDelete(t *testing.T) {
	t.Parallel()

	listServer, listSeen := vulnRecorder(t, http.StatusOK, `[{"id":"H-1","type":"LOGIN","name":"a","username":"u","password":"S-1"},{"id":"K-0","type":"API_KEY","name":"b","password":"S-2"}]`)
	list, _, err := newTestClient(t, listServer.URL).Credentials.ListOrgHTTP(context.Background())
	if err != nil || len(list) != 2 || list[1].Type != "API_KEY" || list[1].Username != "" {
		t.Fatalf("ListOrgHTTP = %+v, %v", list, err)
	}
	if g := (*listSeen)[0]; g.method != http.MethodGet || g.uri != "/api/http-credentials" {
		t.Fatalf("list sent %+v", g)
	}

	delServer, delSeen := vulnRecorder(t, http.StatusNoContent, "")
	if _, err := newTestClient(t, delServer.URL).Credentials.DeleteOrgHTTP(context.Background(), "H-1"); err != nil {
		t.Fatal(err)
	}
	if g := (*delSeen)[0]; g.method != http.MethodDelete || g.uri != "/api/http-credentials/H-1" {
		t.Fatalf("delete sent %+v", g)
	}

	// Forward refuses the delete while a scanner uses the credential; that is the caller's to see, not swallowed.
	busy, _ := vulnRecorder(t, http.StatusBadRequest, `{"httpStatusCode":400,"message":"HTTP Credential H-1 is used by 1 sources, e.g.: tvm"}`)
	if _, err := newTestClient(t, busy.URL).Credentials.DeleteOrgHTTP(context.Background(), "H-1"); !IsStatus(err, http.StatusBadRequest) {
		t.Fatalf("a refused delete = %v, want a 400", err)
	}
}
