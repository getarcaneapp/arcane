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

	// Policy schedules backups of one subject: a volume, a project, or, with no SubjectID, the projects a global project
	// policy selects.
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

	// PolicyOptions holds the settings only some subjects' policies have.
	PolicyOptions struct {
		// Volumes selects a project policy's volumes; nil follows the project's volumes.
		Volumes []string `json:"volumes"`
		// SelectionMode and ProjectIDs select the projects of a global project policy.
		SelectionMode types.SystemVolumeSelectionMode `json:"selectionMode"`
		ProjectIDs    []string                        `json:"projectIds"`
	}
)

const (
	PolicyVolume  = "volume"
	PolicyProject = "project"
)

func (SystemBackupRecoveryConfig) TableName() string { return "system_backup_recovery_config" }

func (Policy) TableName() string { return "backup_policies" }
