package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBackupsListAndProgress(t *testing.T) {
	t.Parallel()

	var progressBody, restoreBody = `null`, ``
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/api/backups":
			_, _ = io.WriteString(w, `[{"id":41,"backupTimeMillis":1759395000000,"storageType":"S3","triggerType":"SCHEDULED","appVersion":"26.9.1","snapshotsIncluded":true,"size":52428800,"note":"nightly"},
			  {"id":40,"backupTimeMillis":1759308600000,"storageType":"INTERNAL","triggerType":"MANUAL","appVersion":"26.9.1","snapshotsIncluded":false,"size":1024}]`)
		case "/api/backups?view=progress":
			_, _ = io.WriteString(w, progressBody)
		case "/api/backups?view=lastRestoreResult":
			if restoreBody == "" {
				w.WriteHeader(http.StatusOK)
				return
			}
			_, _ = io.WriteString(w, restoreBody)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	backups := newTestClient(t, server.URL).Backups
	ctx := context.Background()

	list, _, err := backups.ListBackups(ctx)
	if err != nil || len(list) != 2 || list[0].ID != 41 || list[0].StorageType != StorageTypeS3 || !list[0].SnapshotsIncluded || list[0].Size != 52428800 || list[0].Note != "nightly" ||
		list[0].AppVersion != "26.9.1" || !list[0].BackupTime.Equal(time.UnixMilli(1759395000000)) {
		t.Fatalf("ListBackups() = %+v, %v", list, err)
	}
	// Nothing running is a null, not an error and not a zero operation.
	if op, _, err := backups.BackupProgress(ctx); op != nil || err != nil {
		t.Fatalf("idle BackupProgress() = %+v, %v", op, err)
	}
	progressBody = `{"id":"12","type":"restore","appStatus":"IN_PROGRESS","snapshotToStatus":{"13790":"DONE","13791":"NOT_STARTED"}}`
	op, _, err := backups.BackupProgress(ctx)
	if err != nil || op.Type != "restore" || op.AppStatus != CBRInProgress || op.SnapshotToStatus["13790"] != CBRDone || op.ID != "12" {
		t.Fatalf("BackupProgress() = %+v, %v", op, err)
	}
	if res, _, err := backups.LastRestoreResult(ctx); res != nil || err != nil {
		t.Fatalf("no restore yet: %+v, %v", res, err)
	}
}

// Cancel takes no id (it stops whatever runs), and delete must always state
// where the backup goes away from: there is no default storage.
func TestBackupsCancelAndDelete(t *testing.T) {
	t.Parallel()

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	backups := newTestClient(t, server.URL).Backups
	ctx := context.Background()

	if _, err := backups.CancelOperation(ctx); err != nil || calls[0] != "POST /api/backups?action=cancel" {
		t.Fatalf("CancelOperation: %v %q", err, calls)
	}
	if _, err := backups.DeleteBackup(ctx, 41, StorageTypeS3); err != nil || calls[1] != "DELETE /api/backups/41?storageType=S3" {
		t.Fatalf("delete from S3: %v %q", err, calls[1])
	}
	if _, err := backups.DeleteBackup(ctx, 41, StorageTypeAll); err != nil || calls[2] != "DELETE /api/backups/41" {
		t.Fatalf("delete everywhere sends no storage parameter: %v %q", err, calls[2])
	}
	before := len(calls)
	for name, f := range map[string]func() error{
		"no storage choice": func() error { _, err := backups.DeleteBackup(ctx, 41, ""); return err },
		"unknown storage":   func() error { _, err := backups.DeleteBackup(ctx, 41, "TAPE"); return err },
		"zero id":           func() error { _, err := backups.DeleteBackup(ctx, 0, StorageTypeAll); return err },
	} {
		if err := f(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if len(calls) != before {
		t.Fatalf("refused deletes reached the wire: %q", calls[before:])
	}
}

func TestBackupsRestoreSnapshot(t *testing.T) {
	t.Parallel()

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/snapshots/13790":
			_, _ = io.WriteString(w, `{"status":"NOT_STARTED","startMillis":1759395000000}`)
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"message":"Snapshot 13791 already present on disk"}`)
		case r.URL.Path == "/api/snapshots/none":
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = io.WriteString(w, `{"status":"IN_PROGRESS","startMillis":1759395000000,"stage":"DOWNLOADING","maxProgress":1000,"currentProgress":250}`)
		}
	}))
	defer server.Close()
	backups := newTestClient(t, server.URL).Backups
	ctx := context.Background()

	started, _, err := backups.RestoreSnapshot(ctx, "13790")
	if err != nil || started.Status != CBRNotStarted || started.Finished() || !started.Start.Equal(time.UnixMilli(1759395000000)) || calls[0] != "POST /api/snapshots/13790?action=restore" {
		t.Fatalf("RestoreSnapshot() = %+v, %v; %q", started, err, calls[0])
	}
	if _, _, err := backups.RestoreSnapshot(ctx, "13791"); !IsStatus(err, http.StatusConflict) {
		t.Fatalf("a snapshot already on disk must surface its 409: %v", err)
	}
	status, _, err := backups.SnapshotRestoreStatus(ctx, "13790")
	if err != nil || status.Status != CBRInProgress || status.Stage != "DOWNLOADING" || *status.CurrentProgress != 250 || *status.MaxProgress != 1000 || status.Finished() {
		t.Fatalf("SnapshotRestoreStatus() = %+v, %v", status, err)
	}
	if calls[len(calls)-1] != "GET /api/snapshots/13790?view=restoreStatus" {
		t.Fatalf("status request = %q", calls[len(calls)-1])
	}
	if none, _, err := backups.SnapshotRestoreStatus(ctx, "none"); none != nil || err != nil {
		t.Fatalf("204 means no restore: %+v, %v", none, err)
	}
	if (SnapshotRestoreStatus{Status: CBRDone}).Finished() != true || !(SnapshotRestoreStatus{Status: CBRDone}).Done() || (SnapshotRestoreStatus{Status: CBRError}).Done() {
		t.Fatal("Done and Finished disagree with the status values")
	}
	if _, _, err := backups.RestoreSnapshot(ctx, " "); err == nil {
		t.Fatal("a blank snapshot ID must be refused")
	}
}
