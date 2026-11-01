package types

import (
	"strings"
	"time"
)

type (
	// BackupFileEntry describes one file or directory under a backup's logical
	// restore root.
	BackupFileEntry struct {
		Path        string `json:"path"`
		Name        string `json:"name"`
		IsDirectory bool   `json:"isDirectory"`
	}

	// BackupRestoreSelection selects explicit paths or every eligible backup entry.
	BackupRestoreSelection struct {
		Search    string   `json:"search,omitempty"`
		Paths     []string `json:"paths,omitempty"`
		SelectAll bool     `json:"selectAll,omitempty"`
	}

	// BackupRepositoryObservation describes a read-only S3 metadata check.
	BackupRepositoryObservation struct {
		Reason            string
		Available         bool
		SnapshotAvailable bool
	}

	// BackupRequester is who started a manual backup; its workflow re-checks their permission on every delivery.
	BackupRequester struct {
		UserID        string `json:"userId"`
		APIKeyID      string `json:"apiKeyId,omitempty"`
		EnvironmentID string `json:"environmentId"`
		Permission    string `json:"permission"`
	}

	// S3DestinationUsage reports whether a destination is referenced by backups.
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

	// UpdateBackupPolicy is the shared policy-update payload for every backup
	// domain; StopContainers only applies to volume backups.
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

	// RestoreSystemBackupFilesRequest selects project files to restore from a system backup.
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

	// BackupManagementType identifies which Arcane configuration orchestrated a backup.
	BackupManagementType string

	// SystemVolumeSelectionMode controls how configured volume names affect live Docker volumes.
	SystemVolumeSelectionMode string

	// SystemVolumeBackupPolicy is one manager-owned policy for local Docker volumes.
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

	// UpdateSystemVolumeBackupPolicy is the writable centralized policy shape.
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

	// SystemVolumeBackupCustomRun is a transient policy used by Create -> Backup.
	SystemVolumeBackupCustomRun struct {
		Destination     SystemBackupDestination   `json:"destination"`
		S3DestinationID string                    `json:"s3DestinationId,omitempty"`
		SelectionMode   SystemVolumeSelectionMode `json:"selectionMode"`
		VolumeNames     []string                  `json:"volumeNames"`
		StopContainers  bool                      `json:"stopContainers"`
		IgnoreAnonymous bool                      `json:"ignoreAnonymous"`
	}

	// RunSystemVolumeBackupsRequest selects a saved policy or supplies a transient custom policy.
	RunSystemVolumeBackupsRequest struct {
		Custom   *SystemVolumeBackupCustomRun `json:"custom,omitempty"`
		PolicyID string                       `json:"policyId,omitempty"`
	}

	// SystemVolumeBackupOption describes a selectable live or unavailable configured volume.
	SystemVolumeBackupOption struct {
		Name      string `json:"name"`
		Anonymous bool   `json:"anonymous"`
		Available bool   `json:"available"`
	}

	// SystemVolumeBackupFailure records one candidate that could not be backed up.
	SystemVolumeBackupFailure struct {
		VolumeName string `json:"volumeName"`
		Error      string `json:"error"`
	}

	// SystemVolumeBackupRunResult summarizes a centralized multi-volume execution.
	SystemVolumeBackupRunResult struct {
		Failures  []SystemVolumeBackupFailure `json:"failures"`
		Matched   int                         `json:"matched"`
		Succeeded int                         `json:"succeeded"`
		Failed    int                         `json:"failed"`
		Skipped   int                         `json:"skipped"`
	}

	// SystemVolumeBackupHistoryEntry is a common view over Arcane recovery and volume backup records.
	SystemVolumeBackupHistoryEntry struct {
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

	// BackupRunAccepted identifies a backup operation accepted for background execution.
	BackupRunAccepted struct {
		ActivityID string `json:"activityId"`
		Status     string `json:"status"`
	}
)

const (
	SystemBackupDestinationLocal   SystemBackupDestination = "local"
	SystemBackupDestinationS3      SystemBackupDestination = "s3"
	SystemBackupDestinationLocalS3 SystemBackupDestination = "local_s3"

	// System backup run statuses and triggers.
	SystemBackupStatusRunning    = "running"
	SystemBackupStatusSucceeded  = "succeeded"
	SystemBackupStatusFailed     = "failed"
	SystemBackupTriggerManual    = "manual"
	SystemBackupTriggerScheduled = "scheduled"
	SystemBackupTriggerSafety    = "safety"

	BackupManagementTypeSystem BackupManagementType = "system"
	BackupManagementTypeVolume BackupManagementType = "volume"
	// SystemVolumePolicyPrefix marks volume-backup records created by the centralized system policy.
	SystemVolumePolicyPrefix                                 = "system-volume:"
	SystemVolumeSelectionAll       SystemVolumeSelectionMode = "all"
	SystemVolumeSelectionAllowlist SystemVolumeSelectionMode = "allowlist"
	SystemVolumeSelectionBlocklist SystemVolumeSelectionMode = "blocklist"

	// Volume backup run statuses and triggers.
	VolumeBackupStatusRunning    = "running"
	VolumeBackupStatusSucceeded  = "succeeded"
	VolumeBackupStatusFailed     = "failed"
	VolumeBackupTriggerManual    = "manual"
	VolumeBackupTriggerScheduled = "scheduled"
	VolumeBackupTriggerSafety    = "safety"
)

// BackupManagementTypeForPolicy reports which configuration orchestrated a backup created under policyID.
func BackupManagementTypeForPolicy(policyID string) BackupManagementType {
	if strings.HasPrefix(policyID, SystemVolumePolicyPrefix) {
		return BackupManagementTypeSystem
	}
	return BackupManagementTypeVolume
}
