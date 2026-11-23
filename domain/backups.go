package domain

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
