package types

import (
	"strings"
	"time"
)

// BackupFileEntry describes one file or directory under a backup's logical
// restore root.
type BackupFileEntry struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	IsDirectory bool   `json:"isDirectory"`
}

// BackupRestoreSelection selects explicit paths or every eligible backup entry.
type BackupRestoreSelection struct {
	Paths     []string `json:"paths,omitempty"`
	SelectAll bool     `json:"selectAll,omitempty"`
	Search    string   `json:"search,omitempty"`
}

// BackupRepositoryObservation describes a read-only S3 metadata check.
type BackupRepositoryObservation struct {
	Available         bool
	Reason            string
	SnapshotAvailable bool
}

// BackupRequester is who started a manual backup; its workflow re-checks their permission on every delivery.
type BackupRequester struct {
	UserID        string `json:"userId"`
	APIKeyID      string `json:"apiKeyId,omitempty"`
	EnvironmentID string `json:"environmentId"`
	Permission    string `json:"permission"`
}

// S3DestinationUsage reports whether a destination is referenced by backups.
type S3DestinationUsage struct {
	InUse bool `json:"inUse"`
}

type S3Destination struct {
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
	CreatedAt        time.Time  `json:"createdAt" readOnly:"true"`
	UpdatedAt        *time.Time `json:"updatedAt,omitempty" readOnly:"true"`
}

type S3DestinationSyncRequest struct {
	Destinations []S3Destination `json:"destinations"`
}

type SystemBackupDestination string

const (
	SystemBackupDestinationLocal   SystemBackupDestination = "local"
	SystemBackupDestinationS3      SystemBackupDestination = "s3"
	SystemBackupDestinationLocalS3 SystemBackupDestination = "local_s3"
)

// System backup run statuses and triggers.
const (
	SystemBackupStatusRunning   = "running"
	SystemBackupStatusSucceeded = "succeeded"
	SystemBackupStatusFailed    = "failed"

	SystemBackupTriggerManual    = "manual"
	SystemBackupTriggerScheduled = "scheduled"
	SystemBackupTriggerSafety    = "safety"
)

type SystemBackupRun struct {
	RemoteAvailable   *bool                   `json:"remoteAvailable,omitempty"`
	ActivityID        string                  `json:"activityId,omitempty"`
	ID                string                  `json:"id"`
	Size              int64                   `json:"size"`
	CreatedAt         time.Time               `json:"createdAt"`
	Status            string                  `json:"status"`
	Trigger           string                  `json:"trigger"`
	Destination       SystemBackupDestination `json:"destination"`
	LocalSnapshotID   string                  `json:"localSnapshotId,omitempty"`
	RemoteSnapshotID  string                  `json:"remoteSnapshotId,omitempty"`
	S3DestinationID   string                  `json:"s3DestinationId,omitempty"`
	S3DestinationName string                  `json:"s3DestinationName,omitempty"`
	PolicyID          string                  `json:"policyId,omitempty"`
	Error             string                  `json:"error,omitempty"`
}

type SystemBackupPolicy struct {
	ID                string           `json:"id"`
	Enabled           bool             `json:"enabled"`
	Schedule          string           `json:"schedule"`
	RetentionCount    int              `json:"retentionCount"`
	LocalEnabled      bool             `json:"localEnabled"`
	S3Enabled         bool             `json:"s3Enabled"`
	S3DestinationID   string           `json:"s3DestinationId,omitempty"`
	S3DestinationName string           `json:"s3DestinationName,omitempty"`
	LastRun           *SystemBackupRun `json:"lastRun,omitempty"`
}

// UpdateBackupPolicy is the shared policy-update payload for every backup
// domain; StopContainers only applies to volume backups.
type UpdateBackupPolicy struct {
	ID              string `json:"id,omitempty"`
	Enabled         bool   `json:"enabled"`
	Schedule        string `json:"schedule"`
	RetentionCount  int    `json:"retentionCount"`
	StopContainers  bool   `json:"stopContainers,omitempty"`
	LocalEnabled    bool   `json:"localEnabled"`
	S3Enabled       bool   `json:"s3Enabled"`
	S3DestinationID string `json:"s3DestinationId,omitempty"`
}

type SystemBackupPolicyCollection struct {
	Policies          []SystemBackupPolicy `json:"policies"`
	RecoveryKeyStored bool                 `json:"recoveryKeyStored"`
}

type UpdateSystemBackupPolicies struct {
	Policies []UpdateBackupPolicy `json:"policies"`
}

type SystemBackupRecoveryKeyStatus struct {
	Configured bool `json:"configured"`
}

type SystemBackupRecoveryKey struct {
	RecoveryKey string `json:"recoveryKey"`
}

type CreateSystemBackupRequest struct {
	Destination     SystemBackupDestination `json:"destination,omitempty"`
	S3DestinationID string                  `json:"s3DestinationId,omitempty"`
	RecoveryKey     string                  `json:"recoveryKey,omitempty"`
	PolicyID        string                  `json:"policyId,omitempty"`
}

// RestoreSystemBackupFilesRequest selects project files to restore from a system backup.
type RestoreSystemBackupFilesRequest struct {
	BackupRestoreSelection

	RecoveryKey string `json:"recoveryKey"`
}

type UploadSystemBackupRequest struct {
	S3DestinationID string `json:"s3DestinationId"`
	RecoveryKey     string `json:"recoveryKey"`
}

type DeleteSystemBackupRequest struct {
	RecoveryKey string `json:"recoveryKey,omitempty"`
}

type DiscoverSystemBackupsRequest struct {
	S3DestinationID string `json:"s3DestinationId"`
	RecoveryKey     string `json:"recoveryKey"`
}

// BackupManagementType identifies which Arcane configuration orchestrated a backup.
type BackupManagementType string

const (
	BackupManagementTypeSystem BackupManagementType = "system"
	BackupManagementTypeVolume BackupManagementType = "volume"

	// SystemVolumePolicyPrefix marks volume-backup records created by the centralized system policy.
	SystemVolumePolicyPrefix = "system-volume:"

	SystemVolumeSelectionAll       SystemVolumeSelectionMode = "all"
	SystemVolumeSelectionAllowlist SystemVolumeSelectionMode = "allowlist"
	SystemVolumeSelectionBlocklist SystemVolumeSelectionMode = "blocklist"
)

// SystemVolumeSelectionMode controls how configured volume names affect live Docker volumes.
type SystemVolumeSelectionMode string

// SystemVolumeBackupPolicy is one manager-owned policy for local Docker volumes.
type SystemVolumeBackupPolicy struct {
	ID                string                    `json:"id"`
	Enabled           bool                      `json:"enabled"`
	Schedule          string                    `json:"schedule"`
	RetentionCount    int                       `json:"retentionCount"`
	StopContainers    bool                      `json:"stopContainers"`
	LocalEnabled      bool                      `json:"localEnabled"`
	S3Enabled         bool                      `json:"s3Enabled"`
	S3DestinationID   string                    `json:"s3DestinationId,omitempty"`
	S3DestinationName string                    `json:"s3DestinationName,omitempty"`
	SelectionMode     SystemVolumeSelectionMode `json:"selectionMode"`
	VolumeNames       []string                  `json:"volumeNames"`
	IgnoreAnonymous   bool                      `json:"ignoreAnonymous"`
	LastRun           *SystemBackupRun          `json:"lastRun,omitempty"`
}

// UpdateSystemVolumeBackupPolicy is the writable centralized policy shape.
type UpdateSystemVolumeBackupPolicy struct {
	UpdateBackupPolicy

	SelectionMode   SystemVolumeSelectionMode `json:"selectionMode"`
	VolumeNames     []string                  `json:"volumeNames"`
	IgnoreAnonymous bool                      `json:"ignoreAnonymous"`
}

type SystemVolumeBackupPolicyCollection struct {
	Policies []SystemVolumeBackupPolicy `json:"policies"`
}

type UpdateSystemVolumeBackupPolicies struct {
	Policies []UpdateSystemVolumeBackupPolicy `json:"policies"`
}

// SystemVolumeBackupCustomRun is a transient policy used by Create -> Backup.
type SystemVolumeBackupCustomRun struct {
	Destination     SystemBackupDestination   `json:"destination"`
	S3DestinationID string                    `json:"s3DestinationId,omitempty"`
	StopContainers  bool                      `json:"stopContainers"`
	SelectionMode   SystemVolumeSelectionMode `json:"selectionMode"`
	VolumeNames     []string                  `json:"volumeNames"`
	IgnoreAnonymous bool                      `json:"ignoreAnonymous"`
}

// RunSystemVolumeBackupsRequest selects a saved policy or supplies a transient custom policy.
type RunSystemVolumeBackupsRequest struct {
	PolicyID string                       `json:"policyId,omitempty"`
	Custom   *SystemVolumeBackupCustomRun `json:"custom,omitempty"`
}

// SystemVolumeBackupOption describes a selectable live or unavailable configured volume.
type SystemVolumeBackupOption struct {
	Name      string `json:"name"`
	Anonymous bool   `json:"anonymous"`
	Available bool   `json:"available"`
}

// SystemVolumeBackupFailure records one candidate that could not be backed up.
type SystemVolumeBackupFailure struct {
	VolumeName string `json:"volumeName"`
	Error      string `json:"error"`
}

// SystemVolumeBackupRunResult summarizes a centralized multi-volume execution.
type SystemVolumeBackupRunResult struct {
	Matched   int                         `json:"matched"`
	Succeeded int                         `json:"succeeded"`
	Failed    int                         `json:"failed"`
	Skipped   int                         `json:"skipped"`
	Failures  []SystemVolumeBackupFailure `json:"failures"`
}

// SystemVolumeBackupHistoryEntry is a common view over Arcane recovery and volume backup records.
type SystemVolumeBackupHistoryEntry struct {
	RemoteAvailable   *bool                   `json:"remoteAvailable,omitempty" gorm:"-"`
	ID                string                  `json:"id" sortable:"true"`
	Size              int64                   `json:"size" sortable:"true"`
	CreatedAt         time.Time               `json:"createdAt" sortable:"true"`
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
}

// BackupRunAccepted identifies a backup operation accepted for background execution.
type BackupRunAccepted struct {
	ActivityID string `json:"activityId"`
	Status     string `json:"status"`
}

// Volume backup run statuses and triggers.
const (
	VolumeBackupStatusRunning   = "running"
	VolumeBackupStatusSucceeded = "succeeded"
	VolumeBackupStatusFailed    = "failed"

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
