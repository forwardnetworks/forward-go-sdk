package forward

import (
	"context"
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

	if _, _, err := logs.List(ctx, AuditLogListOptions{}); err != nil || uri != "/api/audit-logs" {
		t.Fatalf("no options must send no parameter (so Forward applies its own paging): %v %q", err, uri)
	}
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.FixedZone("x", 2*3600)) // 07:00Z
	end := time.Date(2026, 10, 2, 0, 0, 0, 500_000_000, time.UTC)
	_, _, err := logs.List(ctx, AuditLogListOptions{
		StartTime: start, EndTime: end, RemoteIP: "10.1.", UserID: "12", ImpersonatorID: "3", HTTPMethod: "delete",
		TargetURI: "/api/networks/9/classic-devices", HTTPResponseCode: Ptr(204), Limit: 50, Offset: 100,
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

func TestAuditLogsListRefusals(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	logs := newTestClient(t, server.URL).AuditLogs
	now := time.Now()
	for name, options := range map[string]AuditLogListOptions{
		"limit over the maximum": {Limit: AuditLogMaxLimit + 1},
		"negative limit":         {Limit: -1},
		"negative offset":        {Offset: -1},
		"unaudited method":       {HTTPMethod: "HEAD"},
		"end before start":       {StartTime: now, EndTime: now.Add(-time.Hour)},
	} {
		if _, _, err := logs.List(context.Background(), options); err == nil {
			t.Errorf("%s must be refused locally", name)
		}
	}
	if calls != 0 {
		t.Fatalf("refused options reached the wire %d times", calls)
	}
}

func TestAuditLogsListDecodes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"records":[
		  {"timestamp":"2026-10-01T12:34:56.789Z","remoteIp":"10.1.2.3","userId":"12","impersonated":false,"httpMethod":"POST","targetUri":"/networks/9/classic-devices","httpResponseCode":201},
		  {"timestamp":1759322096789,"remoteIp":"10.1.2.4","userId":"13","impersonatorId":"3","impersonatorUsername":"support@fwd","impersonated":true,"httpMethod":"DELETE","targetUri":"/networks/9/classic-devices/r1?force=true","httpResponseCode":404},
		  {"remoteIp":"10.1.2.5","impersonated":true,"httpMethod":"PUT","targetUri":"/x","httpResponseCode":200}],
		  "paging":{"offset":0,"limit":1000,"total":3214}}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).AuditLogs.List(context.Background(), AuditLogListOptions{})
	if err != nil || len(got.Records) != 3 || got.Paging.Total != 3214 || got.Paging.Limit != 1000 {
		t.Fatalf("List() = %+v, %v", got, err)
	}
	first, second, third := got.Records[0], got.Records[1], got.Records[2]
	if want := time.Date(2026, 10, 1, 12, 34, 56, 789_000_000, time.UTC); !first.Time.Equal(want) || first.UserID != "12" || first.HTTPMethod != "POST" || first.HTTPResponseCode != 201 {
		t.Fatalf("first = %+v", first)
	}
	if !second.Time.Equal(time.UnixMilli(1759322096789)) || second.ImpersonatorID != "3" || second.ImpersonatorUsername != "support@fwd" || !second.Impersonated {
		t.Fatalf("epoch-millis timestamp or impersonation = %+v", second)
	}
	// A non-Forward viewer sees only that an impersonator was involved.
	if !third.Time.IsZero() || third.ImpersonatorID != "" || !third.Impersonated || third.UserID != "" {
		t.Fatalf("third = %+v", third)
	}
}
