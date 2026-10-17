package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The scheduler's debug view is the one place running and queued tasks show with their worker class, grouped by worker type, priority and
// job, so a reader must be able to pull a task's status and class out of it and still see fields it does not model.
func TestJobsDebugView(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/debug/jobs" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"COMPUTE":{"HIGH":{"idToJobDetails":{"j1":{"jobType":"PREDICT","orgId":"7","priority":"HIGH","extra":1,
			"tasks":[{"taskStatus":"RUNNING","resourceExpectancy":"LARGE","lifeExpectancy":"LONG","worker":{"name":"w1"}},{"taskStatus":"QUEUED","resourceExpectancy":"REGULAR"}]}}}},"SEARCH":{}}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	view, _, err := client.Jobs.DebugView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	job := view["COMPUTE"]["HIGH"].IDToJobDetails["j1"]
	if job.JobType != "PREDICT" || string(job.OrgID) != `"7"` || len(job.Tasks) != 2 || job.Tasks[0].TaskStatus != "RUNNING" ||
		job.Tasks[0].ResourceExpectancy != "LARGE" || job.Tasks[1].TaskStatus != "QUEUED" || len(job.Tasks[1].Worker) != 0 {
		t.Fatalf("decoded %+v", job)
	}
	if len(job.Raw) == 0 || len(view["SEARCH"]) != 0 {
		t.Fatalf("raw or search view wrong: %s", job.Raw)
	}
}
