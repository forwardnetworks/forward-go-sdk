package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Forward's path search takes a domain (PathSearchController: RequestParam "domain"); the request must carry it, and
// must send no parameter when it is unset so the appserver applies its own default.
func TestPathSearchDomainParameter(t *testing.T) {
	t.Parallel()

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Encode()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)

	if _, _, err := c.Networks.Paths(context.Background(), "n1", PathSearchRequest{SrcIP: "10.0.0.1", DstIP: "10.1.0.1", Domain: "example.com"}); err != nil {
		t.Fatal(err)
	}
	if got := parsedQuery(t, query).Get("domain"); got != "example.com" {
		t.Errorf("domain = %q in %q", got, query)
	}
	if _, _, err := c.Networks.Paths(context.Background(), "n1", PathSearchRequest{SrcIP: "10.0.0.1", DstIP: "10.1.0.1"}); err != nil {
		t.Fatal(err)
	}
	if parsedQuery(t, query).Has("domain") {
		t.Errorf("an unset domain must send no parameter, got %q", query)
	}
}

// The S3 secret key is write-only. Forward never serializes it, and even if a server did, the SDK must not hand it back.
func TestS3SecretKeyIsNeverReturned(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"accessKey":"AK","secretKey":"leaked","bucketName":"b","serviceEndpoint":"https://s3"}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)

	got, _, err := c.Backups.GetS3Storage(context.Background())
	if err != nil || got == nil || got.SecretKey != "" || got.AccessKey != "AK" {
		t.Errorf("GetS3Storage = %+v, %v", got, err)
	}
	secret := "new"
	updated, _, err := c.Backups.UpdateS3Storage(context.Background(), S3StorageSettingsPatch{SecretKey: &secret})
	if err != nil || updated.SecretKey != "" {
		t.Errorf("UpdateS3Storage = %+v, %v", updated, err)
	}
}

// Every delete that counts a 404 as "already gone" must still fail when the 404 says Forward does not serve the route:
// reporting success there would claim something was deleted that was never reached.
func TestOlderDeletesDoNotTreatAnUnservedRouteAsGone(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"apiUrl":"`+r.URL.Path+`","httpMethod":"DELETE","message":"No endpoint DELETE `+r.URL.Path+`."}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	ctx := context.Background()

	for name, call := range map[string]func() error{
		"Locations.Delete":     func() error { _, err := c.Locations.Delete(ctx, "n1", "l1"); return err },
		"Users.Delete":         func() error { _, err := c.Users.Delete(ctx, "u1"); return err },
		"WanCircuits.Delete":   func() error { _, err := c.WanCircuits.Delete(ctx, "n1", "w1"); return err },
		"DeviceTags.DeleteTag": func() error { _, err := c.DeviceTags.DeleteTag(ctx, "n1", "t1"); return err },
	} {
		if err := call(); !errors.Is(err, ErrEndpointNotServed) {
			t.Errorf("%s with an unserved route = %v, want ErrEndpointNotServed", name, err)
		}
	}
}

func parsedQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return values
}
