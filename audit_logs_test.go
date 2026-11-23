package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuditLogsListRefusals(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	logs := newTestClient(t, server.URL).AuditLogs
	now := time.Now()
	for name, options := range map[string]domain.AuditLogListOptions{
		"limit over the maximum": {Limit: domain.AuditLogMaxLimit + 1},
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

	got, _, err := newTestClient(t, server.URL).AuditLogs.List(context.Background(), domain.AuditLogListOptions{})
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
