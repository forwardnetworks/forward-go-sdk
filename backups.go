package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// BackupsService implements Forward CBR endpoints. These routes intentionally
// live at the appliance root rather than beneath /api.
type BackupsService service

type StorageType string

const (
	StorageTypeInternal StorageType = "INTERNAL"
	StorageTypeS3       StorageType = "S3"
)

type BackupTriggerType string

const (
	BackupTriggerManual    BackupTriggerType = "MANUAL"
	BackupTriggerScheduled BackupTriggerType = "SCHEDULED"
)

type BackupSettings struct {
	Enabled          bool   `json:"enabled"`
	BackupTime       string `json:"backupTime"`
	NumDaysToRetain  int    `json:"numDaysToRetain"`
	IncludeSnapshots bool   `json:"includeSnapshots"`
}

type BackupSettingsPatch struct {
	Enabled          *bool   `json:"enabled,omitempty"`
	BackupTime       *string `json:"backupTime,omitempty"`
	NumDaysToRetain  *int    `json:"numDaysToRetain,omitempty"`
	IncludeSnapshots *bool   `json:"includeSnapshots,omitempty"`
}

type S3StorageSettings struct {
	AccessKey                 string `json:"accessKey"`
	SecretKey                 string `json:"secretKey,omitempty"`
	BucketName                string `json:"bucketName"`
	ServiceEndpoint           string `json:"serviceEndpoint"`
	DisableSSLValidation      bool   `json:"disableSslValidation,omitempty"`
	Certificate               string `json:"certificate,omitempty"`
	WriteOnly                 bool   `json:"writeOnly,omitempty"`
	DisableChecksumValidation bool   `json:"disableChecksumValidation,omitempty"`
}

type S3StorageSettingsPatch struct {
	AccessKey                 *string `json:"accessKey,omitempty"`
	SecretKey                 *string `json:"secretKey,omitempty"`
	BucketName                *string `json:"bucketName,omitempty"`
	ServiceEndpoint           *string `json:"serviceEndpoint,omitempty"`
	DisableSSLValidation      *bool   `json:"disableSslValidation,omitempty"`
	Certificate               *string `json:"certificate,omitempty"`
	WriteOnly                 *bool   `json:"writeOnly,omitempty"`
	DisableChecksumValidation *bool   `json:"disableChecksumValidation,omitempty"`
}

type BackupResult struct {
	ID               int64             `json:"id"`
	AppStatus        string            `json:"appStatus"`
	SnapshotToStatus map[string]string `json:"snapshotToStatus,omitempty"`
}

type BackupTriggerRequest struct {
	StorageType      StorageType
	IncludeSnapshots bool
	Name             string
}

func (s *BackupsService) GetSettings(ctx context.Context, storageType StorageType) (*BackupSettings, *Response, error) {
	if err := s.requireService(); err != nil {
		return nil, nil, err
	}
	path := "/backup-settings?" + storageTypeQuery(storageType).Encode()
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, path, nil, pathScopeBackup, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.GetSettings")
	out := new(BackupSettings)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

func (s *BackupsService) UpdateSettings(ctx context.Context, storageType StorageType, patch BackupSettingsPatch) (*BackupSettings, *Response, error) {
	if err := s.requireService(); err != nil {
		return nil, nil, err
	}
	path := "/backup-settings?" + storageTypeQuery(storageType).Encode()
	req, err := s.client.newScopedJSONRequest(ctx, http.MethodPatch, path, patch, pathScopeBackup, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.UpdateSettings")
	out := new(BackupSettings)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

func (s *BackupsService) GetS3Storage(ctx context.Context) (*S3StorageSettings, *Response, error) {
	if err := s.requireService(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, "/backup-settings/storage?storageType=S3", nil, pathScopeBackup, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.GetS3Storage")
	out := new(optionalValue[S3StorageSettings])
	response, err := s.client.Do(req, out)
	if err == nil && !out.Present {
		return nil, response, nil
	}
	return &out.Value, response, err
}

func (s *BackupsService) UpdateS3Storage(ctx context.Context, patch S3StorageSettingsPatch) (*S3StorageSettings, *Response, error) {
	if err := s.requireService(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newScopedJSONRequest(ctx, http.MethodPatch, "/backup-settings/storage?storageType=S3", patch, pathScopeBackup, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.UpdateS3Storage")
	out := new(S3StorageSettings)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

func (s *BackupsService) SetS3BucketOwnership(ctx context.Context, settings S3StorageSettings) (*Response, error) {
	if err := s.requireService(); err != nil {
		return nil, err
	}
	req, err := s.client.newScopedJSONRequest(ctx, http.MethodPost, "/backup-settings?storageType=S3&action=chown", settings, pathScopeBackup, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Backups.SetS3BucketOwnership")
	return s.client.Do(req, nil)
}

func (s *BackupsService) Trigger(ctx context.Context, input BackupTriggerRequest) (*Response, error) {
	if err := s.requireService(); err != nil {
		return nil, err
	}
	query := storageTypeQuery(input.StorageType)
	query.Set("includeSnapshots", strconv.FormatBool(input.IncludeSnapshots))
	if name := strings.TrimSpace(input.Name); name != "" {
		query.Set("name", name)
	}
	req, err := s.client.newScopedRequest(ctx, http.MethodPost, "/backups?"+query.Encode(), nil, pathScopeBackup, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Backups.Trigger")
	return s.client.Do(req, nil)
}

func (s *BackupsService) Last(ctx context.Context, storageType StorageType, triggerType BackupTriggerType) (*BackupResult, *Response, error) {
	if err := s.requireService(); err != nil {
		return nil, nil, err
	}
	query := storageTypeQuery(storageType)
	query.Set("view", "lastBackupResult")
	query.Set("triggerType", strings.TrimSpace(string(triggerType)))
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, "/backups?"+query.Encode(), nil, pathScopeBackup, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.Last")
	out := new(optionalValue[BackupResult])
	response, err := s.client.Do(req, out)
	if err == nil && !out.Present {
		return nil, response, nil
	}
	return &out.Value, response, err
}

func (s *BackupsService) requireService() error {
	if s == nil || s.client == nil {
		return errors.New("forward: backups service is nil")
	}
	if s.client.authMode != AuthModeService {
		return errors.New("forward: backup operations require a service principal")
	}
	return nil
}

func storageTypeQuery(storageType StorageType) url.Values {
	query := url.Values{}
	query.Set("storageType", strings.TrimSpace(string(storageType)))
	return query
}
