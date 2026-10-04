package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Backup operations and per-snapshot restore (ClusterBackupRestoreController).
// The controller is active only on ON-PREM, Kubernetes deployments that are not
// shared (@ProfileCriteria allOf ON_PREM, K8S; not SHARED_K8S); elsewhere these
// routes are not mapped and Forward answers 404 "No endpoint". Backup
// administration needs the ADMINISTER_SYSTEM system permission, which Forward
// enforces (403, typed: MissingPermission), so unlike the older Backups methods
// these do not insist on a service principal. There is NO route to restore a
// whole cluster; only to restore one snapshot that a backup holds, and to watch
// and cancel operations.

// StorageTypeAll names every storage a backup may sit in, for DeleteBackup.
const StorageTypeAll StorageType = "ALL"

// AppBackup is one stored backup (AppBackup). BackupTime is when it was taken;
// Size is in bytes; AppVersion is the Forward version that wrote it, which
// decides whether a snapshot in it can be restored (not into an older app).
type AppBackup struct {
	ID                int64             `json:"id"`
	BackupTime        time.Time         `json:"-"`
	StorageType       StorageType       `json:"storageType"`
	TriggerType       BackupTriggerType `json:"triggerType"`
	AppVersion        string            `json:"appVersion,omitempty"`
	SnapshotsIncluded bool              `json:"snapshotsIncluded"`
	Size              int64             `json:"size"`
	Note              string            `json:"note,omitempty"`
}

// UnmarshalJSON reads backupTimeMillis as epoch milliseconds (or an ISO-8601
// instant, should a build change it).
func (b *AppBackup) UnmarshalJSON(data []byte) error {
	type plain AppBackup
	wire := struct {
		plain
		BackupTimeMillis json.RawMessage `json:"backupTimeMillis"`
	}{}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	millis, err := decodeEpochMillisOrInstant(wire.BackupTimeMillis)
	if err != nil {
		return fmt.Errorf("forward: backup time: %w", err)
	}
	*b = AppBackup(wire.plain)
	if millis != 0 {
		b.BackupTime = time.UnixMilli(millis).UTC()
	}
	return nil
}

// ListBackups returns the backups Forward holds, in every storage. GET
// /api/backups (ClusterBackupRestoreController.getBackups; ADMINISTER_SYSTEM).
// Preview: not in the published spec.
func (s *BackupsService) ListBackups(ctx context.Context) ([]AppBackup, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/backups", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.ListBackups")
	var out []AppBackup
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// CBROperationStatus values (CbrOperationStatus): NOT_STARTED and IN_PROGRESS
// are live; ERROR, DONE, CANCELLED and NETWORK_MISSING are final.
const (
	CBRNotStarted     = "NOT_STARTED"
	CBRInProgress     = "IN_PROGRESS"
	CBRError          = "ERROR"
	CBRDone           = "DONE"
	CBRCancelled      = "CANCELLED"
	CBRNetworkMissing = "NETWORK_MISSING"
)

// ClusterOperation is the backup or restore that is running now (or just
// finished). Type is "backup" or "restore" (Forward sends it lower-case).
// AppStatus is the state of the application data and SnapshotToStatus that of
// each snapshot involved, both CBR... values.
type ClusterOperation struct {
	ID               Identifier        `json:"id"`
	Type             string            `json:"type"`
	AppStatus        string            `json:"appStatus"`
	SnapshotToStatus map[string]string `json:"snapshotToStatus,omitempty"`
}

// BackupProgress returns the running (or latest) cluster backup or restore, or
// (nil, nil) when there is none. GET /api/backups?view=progress (any signed-in
// user: Forward uses it to show the restore banner). Preview.
func (s *BackupsService) BackupProgress(ctx context.Context) (*ClusterOperation, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/backups?view=progress", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.BackupProgress")
	out := new(optionalValue[ClusterOperation])
	resp, err := s.client.Do(req, out)
	if err != nil || !out.Present {
		return nil, resp, err
	}
	return &out.Value, resp, nil
}

// LastRestoreResult returns how the last completed restore went, or (nil, nil)
// when there has been none. GET /api/backups?view=lastRestoreResult
// (ADMINISTER_SYSTEM). Preview.
func (s *BackupsService) LastRestoreResult(ctx context.Context) (*ClusterOperation, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/backups?view=lastRestoreResult", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.LastRestoreResult")
	out := new(optionalValue[ClusterOperation])
	resp, err := s.client.Do(req, out)
	if err != nil || !out.Present {
		return nil, resp, err
	}
	return &out.Value, resp, nil
}

// CancelOperation cancels the backup or restore that is running, whichever it
// is; with none running it does nothing. It is not addressed by ID. POST
// /api/backups?action=cancel (ADMINISTER_SYSTEM). Preview. Check BackupProgress
// first to know what it would stop.
func (s *BackupsService) CancelOperation(ctx context.Context) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/backups?action=cancel", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Backups.CancelOperation")
	return s.client.Do(req, nil)
}

// DeleteBackup deletes one backup. storageType INTERNAL or S3 deletes it from
// that storage only; StorageTypeAll deletes it from every storage it is in.
// There is no default, so the choice is always stated. There is no undo. DELETE
// /api/backups/{backupId}[?storageType=] (ADMINISTER_SYSTEM; 204). Preview.
func (s *BackupsService) DeleteBackup(ctx context.Context, backupID int64, storageType StorageType) (*Response, error) {
	if backupID <= 0 {
		return nil, errors.New("forward: backup ID is required")
	}
	path := "/api/backups/" + strconv.FormatInt(backupID, 10)
	switch storageType {
	case StorageTypeInternal, StorageTypeS3:
		path += "?" + url.Values{"storageType": []string{string(storageType)}}.Encode()
	case StorageTypeAll:
	default:
		return nil, fmt.Errorf("forward: delete storage must be INTERNAL, S3 or ALL, got %q", storageType)
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Backups.DeleteBackup")
	return s.client.Do(req, nil)
}

// SnapshotRestoreStatus is the progress of restoring one snapshot from a
// backup (RestoreStatus). Stage is DOWNLOADING or UNPACKING while running, with
// CurrentProgress out of MaxProgress; ErrorMessage is set on ERROR. Status is a
// CBR... value. Start is when the restore began.
type SnapshotRestoreStatus struct {
	Status          string    `json:"status"`
	Start           time.Time `json:"-"`
	Stage           string    `json:"stage,omitempty"`
	MaxProgress     *int64    `json:"maxProgress,omitempty"`
	CurrentProgress *int64    `json:"currentProgress,omitempty"`
	ErrorMessage    string    `json:"errorMessage,omitempty"`
}

// UnmarshalJSON reads startMillis as epoch milliseconds (or an ISO instant).
func (r *SnapshotRestoreStatus) UnmarshalJSON(data []byte) error {
	type plain SnapshotRestoreStatus
	wire := struct {
		plain
		StartMillis json.RawMessage `json:"startMillis"`
	}{}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	millis, err := decodeEpochMillisOrInstant(wire.StartMillis)
	if err != nil {
		return fmt.Errorf("forward: restore start: %w", err)
	}
	*r = SnapshotRestoreStatus(wire.plain)
	if millis != 0 {
		r.Start = time.UnixMilli(millis).UTC()
	}
	return nil
}

// Done reports that the restore finished and succeeded.
func (r SnapshotRestoreStatus) Done() bool { return r.Status == CBRDone }

// Finished reports that the restore is over, successfully or not.
func (r SnapshotRestoreStatus) Finished() bool {
	switch r.Status {
	case CBRError, CBRDone, CBRCancelled, CBRNetworkMissing:
		return true
	}
	return false
}

// RestoreSnapshot starts restoring one snapshot from a backup and returns its
// initial status; follow it with SnapshotRestoreStatus. It adds a snapshot back
// to its network and changes no existing one. Forward refuses with 409 when the
// snapshot is already on disk, and 400 when the backup was written by a newer
// Forward than the running one (upgrade first). The snapshot must have been
// backed up. POST /api/snapshots/{snapshotId}?action=restore (network permission
// RESTORE_SNAPSHOT). Preview.
func (s *BackupsService) RestoreSnapshot(ctx context.Context, snapshotID string) (*SnapshotRestoreStatus, *Response, error) {
	path, err := restorePath(snapshotID, "action", "restore")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.RestoreSnapshot")
	out := new(SnapshotRestoreStatus)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// SnapshotRestoreStatus returns how a snapshot's restore is going, or (nil, nil)
// when none has been started (Forward answers 204). GET
// /api/snapshots/{snapshotId}?view=restoreStatus (VIEW_NETWORK_AND_SNAPSHOTS).
// Preview.
func (s *BackupsService) SnapshotRestoreStatus(ctx context.Context, snapshotID string) (*SnapshotRestoreStatus, *Response, error) {
	path, err := restorePath(snapshotID, "view", "restoreStatus")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.SnapshotRestoreStatus")
	out := new(optionalValue[SnapshotRestoreStatus])
	resp, err := s.client.Do(req, out)
	if err != nil || !out.Present {
		return nil, resp, err
	}
	return &out.Value, resp, nil
}

func restorePath(snapshotID, param, value string) (string, error) {
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return "", errors.New("forward: snapshot ID is required")
	}
	return "/api/snapshots/" + url.PathEscape(snapshotID) + "?" + url.Values{param: []string{value}}.Encode(), nil
}
