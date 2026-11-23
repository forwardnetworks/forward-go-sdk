package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

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
