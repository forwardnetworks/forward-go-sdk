package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// BackupsService implements Forward CBR endpoints
// (ClusterBackupRestoreController). They are ordinary /api routes: the
// controller has no servlet of its own and Forward's DispatcherServlet is
// registered at /api/*. This service used to send them to the appliance root
// (/backup-settings, /backups), which Forward never served -- verified against
// ServletInitializer.getServletMappings at primary 15398425a69 and stable
// 67e89c87124.
//
// Every route here needs the ADMINISTER_SYSTEM system permission, not a particular kind of principal: a person's
// login that holds it works, and one that lacks it gets a typed 403. The controller exists only on on-prem
// Kubernetes deployments; elsewhere the routes answer ErrEndpointNotServed.
type Backups service

func (s *Backups) GetSettings(ctx context.Context, storageType ports.StorageType) (*ports.BackupSettings, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.requireClient(); err != nil {
		return nil, nil, err
	}
	path := "/api/backup-settings?" + storageTypeQuery(storageType).Encode()
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, path, nil, pathScopeBackup, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.GetSettings")
	out := new(ports.BackupSettings)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

func (s *Backups) UpdateSettings(ctx context.Context, storageType ports.StorageType, patch ports.BackupSettingsPatch) (*ports.BackupSettings, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.requireClient(); err != nil {
		return nil, nil, err
	}
	path := "/api/backup-settings?" + storageTypeQuery(storageType).Encode()
	req, err := s.client.newScopedJSONRequest(ctx, http.MethodPatch, path, patch, pathScopeBackup, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.UpdateSettings")
	out := new(ports.BackupSettings)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// GetS3Storage returns the stored S3 settings, or nil when none are configured. The secret key is write-only: Forward
// marks S3StorageSettings.secretKey @JsonIgnore, so no response carries it, and the SDK clears SecretKey on every
// decoded response anyway so a caller can never read one back.
func (s *Backups) GetS3Storage(ctx context.Context) (*ports.S3StorageSettings, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.requireClient(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, "/api/backup-settings/storage?storageType=S3", nil, pathScopeBackup, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.GetS3Storage")
	out := new(optionalValue[ports.S3StorageSettings])
	response, err := s.client.Do(req, out)
	if err == nil && !out.Present {
		return nil, response, nil
	}
	out.Value.SecretKey = ""
	return &out.Value, response, err
}

func (s *Backups) UpdateS3Storage(ctx context.Context, patch ports.S3StorageSettingsPatch) (*ports.S3StorageSettings, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.requireClient(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newScopedJSONRequest(ctx, http.MethodPatch, "/api/backup-settings/storage?storageType=S3", patch, pathScopeBackup, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.UpdateS3Storage")
	out := new(ports.S3StorageSettings)
	response, err := s.client.doRequired(req, out)
	out.SecretKey = ""
	return out, response, err
}

func (s *Backups) SetS3BucketOwnership(ctx context.Context, settings ports.S3StorageSettings) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := s.requireClient(); err != nil {
		return nil, err
	}
	req, err := s.client.newScopedJSONRequest(ctx, http.MethodPost, "/api/backup-settings?storageType=S3&action=chown", settings, pathScopeBackup, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Backups.SetS3BucketOwnership")
	return s.client.Do(req, nil)
}

func (s *Backups) Trigger(ctx context.Context, input ports.BackupTriggerRequest) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := s.requireClient(); err != nil {
		return nil, err
	}
	query := storageTypeQuery(input.StorageType)
	query.Set("includeSnapshots", strconv.FormatBool(input.IncludeSnapshots))
	if name := strings.TrimSpace(input.Name); name != "" {
		query.Set("name", name)
	}
	req, err := s.client.newScopedRequest(ctx, http.MethodPost, "/api/backups?"+query.Encode(), nil, pathScopeBackup, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Backups.Trigger")
	return s.client.Do(req, nil)
}

func (s *Backups) Last(ctx context.Context, storageType ports.StorageType, triggerType ports.BackupTriggerType) (*ports.BackupResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.requireClient(); err != nil {
		return nil, nil, err
	}
	query := storageTypeQuery(storageType)
	query.Set("view", "lastBackupResult")
	query.Set("triggerType", strings.TrimSpace(string(triggerType)))
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, "/api/backups?"+query.Encode(), nil, pathScopeBackup, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Backups.Last")
	out := new(optionalValue[ports.BackupResult])
	response, err := s.client.Do(req, out)
	if err == nil && !out.Present {
		return nil, response, nil
	}
	return &out.Value, response, err
}

// requireClient only guards a nil service. These routes used to be refused client-side for any principal but a service
// one; Forward never required that. It requires the ADMINISTER_SYSTEM system permission and answers a typed 403
// (errors.Is(err, ErrPermissionDenied), MissingPermission) when the caller lacks it, so the SDK now lets it decide.
func (s *Backups) requireClient() error {
	if s == nil || s.client == nil {
		return ports.NilServiceError("backups")
	}
	return nil
}

func storageTypeQuery(storageType ports.StorageType) url.Values {
	query := url.Values{}
	query.Set("storageType", strings.TrimSpace(string(storageType)))
	return query
}
