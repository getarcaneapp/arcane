package backup

import (
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/types/v3"
)

type (
	SystemBackupRecoveryConfig struct {
		database.BaseModel

		EncryptedRecoveryKey string `gorm:"column:encrypted_recovery_key;type:text;not null"`
	}

	Policy struct {
		database.BaseModel

		SubjectKind     string        `json:"subjectKind" gorm:"column:subject_kind;not null;index:idx_backup_policies_subject"`
		SubjectID       string        `json:"subjectId" gorm:"column:subject_id;not null;index:idx_backup_policies_subject"`
		Schedule        string        `json:"schedule" gorm:"column:schedule;not null"`
		S3DestinationID string        `json:"s3DestinationId,omitempty" gorm:"column:s3_destination_id;not null"`
		Options         PolicyOptions `json:"options" gorm:"column:options;type:text;not null;serializer:json"`
		RetentionCount  int           `json:"retentionCount" gorm:"column:retention_count;not null"`
		Enabled         bool          `json:"enabled" gorm:"column:enabled;not null"`
		StopContainers  bool          `json:"stopContainers" gorm:"column:stop_containers;not null"`
		LocalEnabled    bool          `json:"localEnabled" gorm:"column:local_enabled;not null"`
		S3Enabled       bool          `json:"s3Enabled" gorm:"column:s3_enabled;not null"`
	}

	RepositoryStats struct {
		database.BaseModel

		SnapshotCount int64 `gorm:"column:snapshot_count;not null"`
		StoredBytes   int64 `gorm:"column:stored_bytes;not null"`
		RawBytes      int64 `gorm:"column:raw_bytes;not null"`
		LogicalBytes  int64 `gorm:"column:logical_bytes;not null"`
	}

	PolicyOptions struct {
		Volumes         []string                        `json:"volumes"`
		SelectionMode   types.SystemVolumeSelectionMode `json:"selectionMode"`
		ProjectIDs      []string                        `json:"projectIds"`
		VolumeSelection types.SystemVolumeSelectionMode `json:"volumeSelectionMode"`
		ProjectVolumes  []types.GlobalProjectVolume     `json:"projectVolumes"`
		VolumeNames     []string                        `json:"volumeNames"`
		IgnoreAnonymous bool                            `json:"ignoreAnonymous"`
	}
)

const (
	PolicySystem  = "system"
	PolicyVolume  = "volume"
	PolicyProject = "project"
)

func (SystemBackupRecoveryConfig) TableName() string { return "system_backup_recovery_config" }

func (Policy) TableName() string { return "backup_policies" }

func (RepositoryStats) TableName() string { return "backup_repository_stats" }
