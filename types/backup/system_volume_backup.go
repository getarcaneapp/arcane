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

// SystemVolumeBackupConfig is the single manager-owned policy for local Docker volumes.
type SystemVolumeBackupConfig struct {
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
	ID                string                  `json:"id"`
	Size              int64                   `json:"size"`
	CreatedAt         time.Time               `json:"createdAt"`
	Status            string                  `json:"status"`
	Trigger           string                  `json:"trigger"`
	Destination       SystemBackupDestination `json:"destination"`
	Format            string                  `json:"format,omitempty"`
	LocalSnapshotID   string                  `json:"localSnapshotId,omitempty"`
	RemoteSnapshotID  string                  `json:"remoteSnapshotId,omitempty"`
	S3DestinationID   string                  `json:"s3DestinationId,omitempty"`
	S3DestinationName string                  `json:"s3DestinationName,omitempty"`
	PolicyID          string                  `json:"policyId,omitempty"`
	Error             string                  `json:"error,omitempty"`
	Type              ManagementType          `json:"type"`
	ResourceType      string                  `json:"resourceType"`
	ResourceName      string                  `json:"resourceName"`
}
