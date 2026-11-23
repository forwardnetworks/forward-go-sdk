package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A record carries the path AFTER /api (the dispatcher servlet's pathInfo),
// so the SDK's own "/api/networks/..." strings would match nothing; List
// drops the prefix, and sends Instants as UTC because Forward's binder
// rejects numeric offsets.
func TestAuditLogsListQuery(t *testing.T) {
	t.Parallel()

	var uri string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri = r.URL.RequestURI()
		_, _ = io.WriteString(w, `{"records":[],"paging":{"offset":0,"limit":1000,"total":0}}`)
	}))
	defer server.Close()
	logs := newTestClient(t, server.URL).AuditLogs
	ctx := context.Background()

	if _, _, err := logs.List(ctx, ports.AuditLogListOptions{}); err != nil || uri != "/api/audit-logs" {
		t.Fatalf("no options must send no parameter (so Forward applies its own paging): %v %q", err, uri)
	}
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.FixedZone("x", 2*3600)) // 07:00Z
	end := time.Date(2026, 10, 2, 0, 0, 0, 500_000_000, time.UTC)
	_, _, err := logs.List(ctx, ports.AuditLogListOptions{
		StartTime: start, EndTime: end, RemoteIP: "10.1.", UserID: "12", ImpersonatorID: "3", HTTPMethod: "delete",
		TargetURI: "/api/networks/9/classic-devices", HTTPResponseCode: ports.Ptr(204), Limit: 50, Offset: 100,
	})
	want := "/api/audit-logs?endTime=2026-10-02T00%3A00%3A00.5Z&httpMethod=DELETE&httpResponseCode=204&impersonatorId=3&limit=50&offset=100" +
		"&remoteIp=10.1.&startTime=2026-10-01T07%3A00%3A00Z&targetUri=%2Fnetworks%2F9%2Fclassic-devices&userId=12"
	if err != nil || uri != want {
		t.Fatalf("query:\n got %s\nwant %s\nerr %v", uri, want, err)
	}
	for in, out := range map[string]string{"/api": "", "/api/networks": "/networks", "/networks": "/networks", "/apiary": "/apiary", " /api/x ": "/x"} {
		if got := auditTargetURI(in); got != out {
			t.Errorf("auditTargetURI(%q) = %q, want %q", in, got, out)
		}
	}
}
