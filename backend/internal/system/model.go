package system

import (
	"time"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/types/v3"
)

type (
	SystemBackupStatus string

	SystemBackupTrigger string

	SystemBackupRun struct {
		database.BaseModel

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

		Size int64 `json:"size" gorm:"column:size" sortable:"true"`
	}
)

const (
	SystemBackupStatusRunning    SystemBackupStatus  = types.SystemBackupStatusRunning
	SystemBackupStatusSucceeded  SystemBackupStatus  = types.SystemBackupStatusSucceeded
	SystemBackupStatusFailed     SystemBackupStatus  = types.SystemBackupStatusFailed
	SystemBackupTriggerManual    SystemBackupTrigger = types.SystemBackupTriggerManual
	SystemBackupTriggerScheduled SystemBackupTrigger = types.SystemBackupTriggerScheduled
	SystemBackupTriggerSafety    SystemBackupTrigger = types.SystemBackupTriggerSafety
	SystemBackupTriggerManager   SystemBackupTrigger = types.SystemBackupTriggerManager
)

func (SystemBackupRun) TableName() string { return "system_backup_runs" }

func (b SystemBackupRun) ToDTO() types.SystemBackupRun {
	return types.SystemBackupRun{
		ID: b.ID, Size: b.Size, CreatedAt: b.CreatedAt, Status: string(b.Status), Trigger: string(b.Trigger),
		Destination: b.Destination, LocalSnapshotID: b.LocalSnapshotID, RemoteSnapshotID: b.RemoteSnapshotID,
		S3DestinationID: b.S3DestinationID, S3DestinationName: b.S3DestinationName, PolicyID: b.PolicyID, Error: b.Error,
	}
}
