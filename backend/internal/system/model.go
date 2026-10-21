package system

import (
	"time"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

type SystemBackupStatus string

const (
	SystemBackupStatusRunning   SystemBackupStatus = types.SystemBackupStatusRunning
	SystemBackupStatusSucceeded SystemBackupStatus = types.SystemBackupStatusSucceeded
	SystemBackupStatusFailed    SystemBackupStatus = types.SystemBackupStatusFailed

	SystemBackupTriggerManual    SystemBackupTrigger = types.SystemBackupTriggerManual
	SystemBackupTriggerScheduled SystemBackupTrigger = types.SystemBackupTriggerScheduled
	SystemBackupTriggerSafety    SystemBackupTrigger = types.SystemBackupTriggerSafety
)

type SystemBackupTrigger string

type SystemBackupRun struct {
	database.BaseModel

	Size              int64                         `json:"size" gorm:"column:size" sortable:"true"`
	CreatedAt         time.Time                     `json:"createdAt" gorm:"column:created_at" sortable:"true"`
	Status            SystemBackupStatus            `json:"status" gorm:"column:status;type:text;not null" sortable:"true"`
	Trigger           SystemBackupTrigger           `json:"trigger" gorm:"column:trigger;type:text;not null" sortable:"true"`
	Destination       types.SystemBackupDestination `json:"destination" gorm:"column:destination;type:text;not null" sortable:"true"`
	LocalSnapshotID   string                        `json:"localSnapshotId,omitempty" gorm:"column:local_snapshot_id;type:text"`
	RemoteSnapshotID  string                        `json:"remoteSnapshotId,omitempty" gorm:"column:remote_snapshot_id;type:text"`
	S3DestinationID   string                        `json:"s3DestinationId,omitempty" gorm:"column:s3_destination_id;type:text;index"`
	S3DestinationName string                        `json:"s3DestinationName,omitempty" gorm:"-"`
	PolicyID          string                        `json:"policyId,omitempty" gorm:"column:policy_id;type:text;index"`
	Error             string                        `json:"error,omitempty" gorm:"column:error;type:text"`
}

func (SystemBackupRun) TableName() string { return "system_backup_runs" }

func (b SystemBackupRun) ToDTO() types.SystemBackupRun {
	return types.SystemBackupRun{
		ID: b.ID, Size: b.Size, CreatedAt: b.CreatedAt, Status: string(b.Status), Trigger: string(b.Trigger),
		Destination: b.Destination, LocalSnapshotID: b.LocalSnapshotID, RemoteSnapshotID: b.RemoteSnapshotID,
		S3DestinationID: b.S3DestinationID, S3DestinationName: b.S3DestinationName, PolicyID: b.PolicyID, Error: b.Error,
	}
}

type SystemBackupPolicy struct {
	database.BaseModel

	Enabled         bool   `gorm:"column:enabled;not null;default:false"`
	Schedule        string `gorm:"column:schedule;type:text;not null"`
	RetentionCount  int    `gorm:"column:retention_count;not null;default:7"`
	LocalEnabled    bool   `gorm:"column:local_enabled;not null;default:true"`
	S3Enabled       bool   `gorm:"column:s3_enabled;not null;default:false"`
	S3DestinationID string `gorm:"column:s3_destination_id;type:text;index"`
}

func (SystemBackupPolicy) TableName() string { return "system_backup_policies" }

func (p SystemBackupPolicy) ToDTO() types.SystemBackupPolicy {
	return types.SystemBackupPolicy{
		ID: p.ID, Enabled: p.Enabled, Schedule: p.Schedule, RetentionCount: p.RetentionCount,
		LocalEnabled: p.LocalEnabled, S3Enabled: p.S3Enabled, S3DestinationID: p.S3DestinationID,
	}
}
