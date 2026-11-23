package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Backup operations and per-snapshot restore (ClusterBackupRestoreController).
// The controller is active only on ON-PREM, Kubernetes deployments that are not
// shared (@ProfileCriteria allOf ON_PREM, K8S; not SHARED_K8S); elsewhere these
// routes are not mapped and Forward answers 404 "No endpoint", which the SDK
// classifies as ErrEndpointNotServed. Backup
// administration needs the ADMINISTER_SYSTEM system permission, which Forward
// enforces (403, typed: MissingPermission); like every Backups method these do
// not insist on a particular principal. There is NO route to restore a whole
// cluster; only to restore one snapshot that a backup holds, and to watch and
// cancel operations.

// ListBackups returns the backups Forward holds, in every storage. GET
// /api/backups (ClusterBackupRestoreController.getBackups; ADMINISTER_SYSTEM).
// Preview: not in the published spec.
func (s *Backups) ListBackups(ctx context.Context) ([]ports.AppBackup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/backups", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.ListBackups")
	var out []ports.AppBackup
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// BackupProgress returns the running (or latest) cluster backup or restore, or
// (nil, nil) when there is none. GET /api/backups?view=progress (any signed-in
// user: Forward uses it to show the restore banner). Preview.
func (s *Backups) BackupProgress(ctx context.Context) (*ports.ClusterOperation, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/backups?view=progress", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.BackupProgress")
	out := new(optionalValue[ports.ClusterOperation])
	resp, err := s.client.Do(req, out)
	if err != nil || !out.Present {
		return nil, resp, err
	}
	return &out.Value, resp, nil
}

// LastRestoreResult returns how the last completed restore went, or (nil, nil)
// when there has been none. GET /api/backups?view=lastRestoreResult
// (ADMINISTER_SYSTEM). Preview.
func (s *Backups) LastRestoreResult(ctx context.Context) (*ports.ClusterOperation, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/backups?view=lastRestoreResult", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.LastRestoreResult")
	out := new(optionalValue[ports.ClusterOperation])
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
func (s *Backups) CancelOperation(ctx context.Context) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *Backups) DeleteBackup(ctx context.Context, backupID int64, storageType ports.StorageType) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if backupID <= 0 {
		return nil, errors.New("forward: backup ID is required")
	}
	path := "/api/backups/" + strconv.FormatInt(backupID, 10)
	switch storageType {
	case ports.StorageTypeInternal, ports.StorageTypeS3:
		path += "?" + url.Values{"storageType": []string{string(storageType)}}.Encode()
	case ports.StorageTypeAll:
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

// RestoreSnapshot starts restoring one snapshot from a backup and returns its
// initial status; follow it with SnapshotRestoreStatus. It adds a snapshot back
// to its network and changes no existing one. Forward refuses with 409 when the
// snapshot is already on disk, and 400 when the backup was written by a newer
// Forward than the running one (upgrade first). The snapshot must have been
// backed up. POST /api/snapshots/{snapshotId}?action=restore (network permission
// RESTORE_SNAPSHOT). Preview.
func (s *Backups) RestoreSnapshot(ctx context.Context, snapshotID string) (*ports.SnapshotRestoreStatus, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := restorePath(snapshotID, "action", "restore")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.RestoreSnapshot")
	out := new(ports.SnapshotRestoreStatus)
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
func (s *Backups) SnapshotRestoreStatus(ctx context.Context, snapshotID string) (*ports.SnapshotRestoreStatus, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := restorePath(snapshotID, "view", "restoreStatus")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.SnapshotRestoreStatus")
	out := new(optionalValue[ports.SnapshotRestoreStatus])
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
