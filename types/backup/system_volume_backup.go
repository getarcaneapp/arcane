package backup

import "time"

// ManagementType identifies which Arcane configuration orchestrated a backup.
type ManagementType string

const (
	ManagementTypeSystem ManagementType = "system"
	ManagementTypeVolume ManagementType = "volume"
)

// SystemVolumePolicyPrefix marks volume-backup records created by the centralized system policy.
const SystemVolumePolicyPrefix = "system-volume:"

// SystemVolumeSelectionMode controls how configured volume names affect live Docker volumes.
type SystemVolumeSelectionMode string

const (
	SystemVolumeSelectionAll       SystemVolumeSelectionMode = "all"
	SystemVolumeSelectionAllowlist SystemVolumeSelectionMode = "allowlist"
	SystemVolumeSelectionBlocklist SystemVolumeSelectionMode = "blocklist"
)

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

// HistoryEntry is a common view over Arcane recovery and volume backup records.
type HistoryEntry struct {
	ID                string                  `json:"id" sortable:"true"`
	Size              int64                   `json:"size" sortable:"true"`
	CreatedAt         time.Time               `json:"createdAt" sortable:"true"`
	Status            string                  `json:"status" sortable:"true"`
	Trigger           string                  `json:"trigger" sortable:"true"`
	Destination       SystemBackupDestination `json:"destination" sortable:"true"`
	Format            string                  `json:"format,omitempty"`
	LocalSnapshotID   string                  `json:"localSnapshotId,omitempty"`
	RemoteSnapshotID  string                  `json:"remoteSnapshotId,omitempty"`
	S3DestinationID   string                  `json:"s3DestinationId,omitempty"`
	S3DestinationName string                  `json:"s3DestinationName,omitempty"`
	PolicyID          string                  `json:"policyId,omitempty"`
	Error             string                  `json:"error,omitempty"`
	Type              ManagementType          `json:"type" sortable:"true"`
	ResourceType      string                  `json:"resourceType"`
	ResourceName      string                  `json:"resourceName" sortable:"true"`
}
