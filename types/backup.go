package types

import (
	"strings"
	"time"
)

type (
	BackupFileEntry struct {
		Path        string `json:"path"`
		Name        string `json:"name"`
		IsDirectory bool   `json:"isDirectory"`
	}

	BackupRestoreSelection struct {
		Search    string   `json:"search,omitempty"`
		Paths     []string `json:"paths,omitempty"`
		SelectAll bool     `json:"selectAll,omitempty"`
	}

	BackupRepositoryObservation struct {
		Reason            string
		Available         bool
		SnapshotAvailable bool
	}

	BackupRequester struct {
		UserID        string `json:"userId"`
		APIKeyID      string `json:"apiKeyId,omitempty"`
		EnvironmentID string `json:"environmentId"`
		Permission    string `json:"permission"`
	}

	S3DestinationUsage struct {
		InUse bool `json:"inUse"`
	}

	S3Destination struct {
		CreatedAt        time.Time  `json:"createdAt" readOnly:"true"`
		UpdatedAt        *time.Time `json:"updatedAt,omitempty" readOnly:"true"`
		ID               string     `json:"id" readOnly:"true"`
		Name             string     `json:"name" required:"false" minLength:"1" unorm:"nfc" trim:"true"`
		Endpoint         string     `json:"endpoint,omitempty"`
		Bucket           string     `json:"bucket" required:"false"`
		Region           string     `json:"region" required:"false"`
		AccessKeyID      string     `json:"accessKeyId" required:"false"`
		SecretAccessKey  string     `json:"secretAccessKey,omitempty" writeOnly:"true"`
		Prefix           string     `json:"prefix,omitempty"`
		UseSSL           bool       `json:"useSsl" required:"false"`
		ForcePathStyle   bool       `json:"forcePathStyle" required:"false"`
		SecretConfigured bool       `json:"secretConfigured" readOnly:"true"`
	}

	S3DestinationSyncRequest struct {
		Destinations []S3Destination `json:"destinations"`
	}

	SystemBackupDestination string

	SystemBackupRun struct {
		CreatedAt         time.Time               `json:"createdAt"`
		RemoteAvailable   *bool                   `json:"remoteAvailable,omitempty"`
		ActivityID        string                  `json:"activityId,omitempty"`
		ID                string                  `json:"id"`
		Status            string                  `json:"status"`
		Trigger           string                  `json:"trigger"`
		Destination       SystemBackupDestination `json:"destination"`
		LocalSnapshotID   string                  `json:"localSnapshotId,omitempty"`
		RemoteSnapshotID  string                  `json:"remoteSnapshotId,omitempty"`
		S3DestinationID   string                  `json:"s3DestinationId,omitempty"`
		S3DestinationName string                  `json:"s3DestinationName,omitempty"`
		PolicyID          string                  `json:"policyId,omitempty"`
		Error             string                  `json:"error,omitempty"`
		Size              int64                   `json:"size"`
	}

	SystemBackupPolicy struct {
		LastRun           *SystemBackupRun `json:"lastRun,omitempty"`
		ID                string           `json:"id"`
		Schedule          string           `json:"schedule"`
		S3DestinationID   string           `json:"s3DestinationId,omitempty"`
		S3DestinationName string           `json:"s3DestinationName,omitempty"`
		RetentionCount    int              `json:"retentionCount"`
		Enabled           bool             `json:"enabled"`
		LocalEnabled      bool             `json:"localEnabled"`
		S3Enabled         bool             `json:"s3Enabled"`
	}

	UpdateBackupPolicy struct {
		ID              string `json:"id,omitempty"`
		Schedule        string `json:"schedule"`
		S3DestinationID string `json:"s3DestinationId,omitempty"`
		RetentionCount  int    `json:"retentionCount"`
		Enabled         bool   `json:"enabled"`
		StopContainers  bool   `json:"stopContainers,omitempty"`
		LocalEnabled    bool   `json:"localEnabled"`
		S3Enabled       bool   `json:"s3Enabled"`
	}

	SystemBackupPolicyCollection struct {
		Policies          []SystemBackupPolicy `json:"policies"`
		RecoveryKeyStored bool                 `json:"recoveryKeyStored"`
	}

	UpdateSystemBackupPolicies struct {
		Policies []UpdateBackupPolicy `json:"policies"`
	}

	SystemBackupRecoveryKeyStatus struct {
		Configured bool `json:"configured"`
	}

	SystemBackupRecoveryKey struct {
		RecoveryKey string `json:"recoveryKey"`
	}

	CreateSystemBackupRequest struct {
		Destination     SystemBackupDestination `json:"destination,omitempty"`
		S3DestinationID string                  `json:"s3DestinationId,omitempty"`
		RecoveryKey     string                  `json:"recoveryKey,omitempty"`
		PolicyID        string                  `json:"policyId,omitempty"`
	}

	RestoreSystemBackupFilesRequest struct {
		RecoveryKey string `json:"recoveryKey"`
		BackupRestoreSelection
	}

	UploadSystemBackupRequest struct {
		S3DestinationID string `json:"s3DestinationId"`
		RecoveryKey     string `json:"recoveryKey"`
	}

	DeleteSystemBackupRequest struct {
		RecoveryKey string `json:"recoveryKey,omitempty"`
	}

	DiscoverSystemBackupsRequest struct {
		S3DestinationID string `json:"s3DestinationId"`
		RecoveryKey     string `json:"recoveryKey"`
	}

	BackupManagementType string

	SystemVolumeSelectionMode string

	SystemVolumeBackupPolicy struct {
		LastRun           *SystemBackupRun          `json:"lastRun,omitempty"`
		ID                string                    `json:"id"`
		Schedule          string                    `json:"schedule"`
		S3DestinationID   string                    `json:"s3DestinationId,omitempty"`
		S3DestinationName string                    `json:"s3DestinationName,omitempty"`
		SelectionMode     SystemVolumeSelectionMode `json:"selectionMode"`
		VolumeNames       []string                  `json:"volumeNames"`
		RetentionCount    int                       `json:"retentionCount"`
		Enabled           bool                      `json:"enabled"`
		StopContainers    bool                      `json:"stopContainers"`
		LocalEnabled      bool                      `json:"localEnabled"`
		S3Enabled         bool                      `json:"s3Enabled"`
		IgnoreAnonymous   bool                      `json:"ignoreAnonymous"`
	}

	UpdateSystemVolumeBackupPolicy struct {
		SelectionMode SystemVolumeSelectionMode `json:"selectionMode"`
		VolumeNames   []string                  `json:"volumeNames"`
		UpdateBackupPolicy

		IgnoreAnonymous bool `json:"ignoreAnonymous"`
	}

	SystemVolumeBackupPolicyCollection struct {
		Policies []SystemVolumeBackupPolicy `json:"policies"`
	}

	UpdateSystemVolumeBackupPolicies struct {
		Policies []UpdateSystemVolumeBackupPolicy `json:"policies"`
	}

	SystemVolumeBackupCustomRun struct {
		Destination     SystemBackupDestination   `json:"destination"`
		S3DestinationID string                    `json:"s3DestinationId,omitempty"`
		SelectionMode   SystemVolumeSelectionMode `json:"selectionMode"`
		VolumeNames     []string                  `json:"volumeNames"`
		StopContainers  bool                      `json:"stopContainers"`
		IgnoreAnonymous bool                      `json:"ignoreAnonymous"`
	}

	RunSystemVolumeBackupsRequest struct {
		Custom   *SystemVolumeBackupCustomRun `json:"custom,omitempty"`
		PolicyID string                       `json:"policyId,omitempty"`
	}

	SystemVolumeBackupOption struct {
		Name      string `json:"name"`
		Anonymous bool   `json:"anonymous"`
		Available bool   `json:"available"`
	}

	SystemVolumeBackupFailure struct {
		VolumeName string `json:"volumeName"`
		Error      string `json:"error"`
	}

	SystemVolumeBackupRunResult struct {
		Failures  []SystemVolumeBackupFailure `json:"failures"`
		Matched   int                         `json:"matched"`
		Succeeded int                         `json:"succeeded"`
		Failed    int                         `json:"failed"`
		Skipped   int                         `json:"skipped"`
	}

	SystemVolumeBackupHistoryEntry struct {
		EnvironmentTag
		CreatedAt         time.Time               `json:"createdAt" sortable:"true"`
		RemoteAvailable   *bool                   `json:"remoteAvailable,omitempty" gorm:"-"`
		ID                string                  `json:"id" sortable:"true"`
		Status            string                  `json:"status" sortable:"true"`
		Trigger           string                  `json:"trigger" sortable:"true"`
		Destination       SystemBackupDestination `json:"destination" sortable:"true"`
		Format            string                  `json:"format,omitempty"`
		LocalSnapshotID   string                  `json:"localSnapshotId,omitempty"`
		RemoteSnapshotID  string                  `json:"remoteSnapshotId,omitempty"`
		RemoteInstanceID  string                  `json:"remoteInstanceId,omitempty"`
		S3DestinationID   string                  `json:"s3DestinationId,omitempty"`
		S3DestinationName string                  `json:"s3DestinationName,omitempty"`
		PolicyID          string                  `json:"policyId,omitempty"`
		Error             string                  `json:"error,omitempty"`
		Type              BackupManagementType    `json:"type" sortable:"true"`
		ResourceType      string                  `json:"resourceType"`
		ResourceName      string                  `json:"resourceName" sortable:"true"`
		Size              int64                   `json:"size" sortable:"true"`
	}

	BackupRunAccepted struct {
		ActivityID string `json:"activityId"`
		Status     string `json:"status"`
	}

	// UnmanagedBackupSweep ages out safety backups and backups whose volume or schedule no longer exists.
	UnmanagedBackupSweep struct {
		Now                   time.Time
		SystemVolumePolicyIDs []string
		Retention             time.Duration
	}
)

const (
	SystemBackupDestinationLocal   SystemBackupDestination = "local"
	SystemBackupDestinationS3      SystemBackupDestination = "s3"
	SystemBackupDestinationLocalS3 SystemBackupDestination = "local_s3"

	SystemBackupStatusRunning    = "running"
	SystemBackupStatusSucceeded  = "succeeded"
	SystemBackupStatusFailed     = "failed"
	SystemBackupTriggerManual    = "manual"
	SystemBackupTriggerScheduled = "scheduled"
	SystemBackupTriggerSafety    = "safety"

	BackupManagementTypeSystem     BackupManagementType      = "system"
	BackupManagementTypeVolume     BackupManagementType      = "volume"
	SystemVolumePolicyPrefix                                 = "system-volume:"
	SystemVolumeSelectionAll       SystemVolumeSelectionMode = "all"
	SystemVolumeSelectionAllowlist SystemVolumeSelectionMode = "allowlist"
	SystemVolumeSelectionBlocklist SystemVolumeSelectionMode = "blocklist"

	VolumeBackupStatusRunning    = "running"
	VolumeBackupStatusSucceeded  = "succeeded"
	VolumeBackupStatusFailed     = "failed"
	VolumeBackupTriggerManual    = "manual"
	VolumeBackupTriggerScheduled = "scheduled"
	VolumeBackupTriggerSafety    = "safety"
)

func BackupManagementTypeForPolicy(policyID string) BackupManagementType {
	if strings.HasPrefix(policyID, SystemVolumePolicyPrefix) {
		return BackupManagementTypeSystem
	}
	return BackupManagementTypeVolume
}
