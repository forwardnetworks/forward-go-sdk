package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A PATCH must carry only what was stated: Forward reads an absent property as "leave it", so a stray collect:false
// would switch collection off. A non-nil empty assume-role list is stated (it clears the roles); nil is not.
func TestCloudAccountPatchPresenceSemantics(t *testing.T) {
	t.Parallel()

	var path string
	var body map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.EscapedPath()
		data, _ := io.ReadAll(r.Body)
		body = nil
		_ = json.Unmarshal(data, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"aws1","type":"AWS"}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	ctx := context.Background()

	if _, _, err := c.CloudAccounts.Patch(ctx, "n1", "aws1", CloudAccountPatch{Type: "AWS", Regions: map[string]int64{"us-east-1": 1760000000000}}); err != nil {
		t.Fatal(err)
	}
	if path != "PATCH /api/networks/n1/cloudAccounts/aws1" || len(body) != 2 || string(body["type"]) != `"AWS"` || string(body["regions"]) != `{"us-east-1":1760000000000}` {
		t.Errorf("regions-only patch = %s %v", path, body)
	}

	empty := []AWSAssumeRoleInfo{}
	off := false
	if _, _, err := c.CloudAccounts.Patch(ctx, "n1", "aws1", CloudAccountPatch{Type: "AWS", Collect: &off, AssumeRoleInfos: &empty, Regions: map[string]int64{}}); err != nil {
		t.Fatal(err)
	}
	if string(body["assumeRoleInfos"]) != "[]" || string(body["collect"]) != "false" {
		t.Errorf("stated empty list and collect=false must be sent: %v", body)
	}
	if _, ok := body["regions"]; ok {
		t.Errorf("an empty regions map is rejected by Forward and must be omitted: %v", body)
	}

	if _, _, err := c.CloudAccounts.Patch(ctx, "n1", "aws1", CloudAccountPatch{}); err == nil {
		t.Error("a patch with no type must be refused before any request")
	}
}
