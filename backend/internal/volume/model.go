package volume

import (
	"time"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
)

type VolumeBackupStatus string

const (
	VolumeBackupStatusRunning   VolumeBackupStatus = types.VolumeBackupStatusRunning
	VolumeBackupStatusSucceeded VolumeBackupStatus = types.VolumeBackupStatusSucceeded
	VolumeBackupStatusFailed    VolumeBackupStatus = types.VolumeBackupStatusFailed

	VolumeBackupTriggerManual    VolumeBackupTrigger = types.VolumeBackupTriggerManual
	VolumeBackupTriggerScheduled VolumeBackupTrigger = types.VolumeBackupTriggerScheduled
	VolumeBackupTriggerSafety    VolumeBackupTrigger = types.VolumeBackupTriggerSafety

	VolumeBackupFormatArchive VolumeBackupFormat = VolumeBackupFormat(types.VolumeBackupFormatArchive)
	VolumeBackupFormatRustic  VolumeBackupFormat = VolumeBackupFormat(types.VolumeBackupFormatRustic)
)

type VolumeBackupTrigger string

type VolumeBackupFormat string

type VolumeBackup struct {
	database.BaseModel

	VolumeName        string                        `json:"volumeName" gorm:"column:volume_name;index"`
	Size              int64                         `json:"size" gorm:"column:size"`
	CreatedAt         time.Time                     `json:"createdAt" gorm:"column:created_at"`
	Status            VolumeBackupStatus            `json:"status" gorm:"column:status;type:text;not null;default:succeeded"`
	Trigger           VolumeBackupTrigger           `json:"trigger" gorm:"column:trigger;type:text;not null;default:manual"`
	Destination       types.VolumeBackupDestination `json:"destination" gorm:"column:destination;type:text;not null;default:local"`
	Format            VolumeBackupFormat            `json:"format" gorm:"column:format;type:text;not null;default:archive"`
	LocalSnapshotID   string                        `json:"localSnapshotId,omitempty" gorm:"column:local_snapshot_id;type:text"`
	RemoteSnapshotID  string                        `json:"remoteSnapshotId,omitempty" gorm:"column:remote_snapshot_id;type:text"`
	S3DestinationID   string                        `json:"s3DestinationId,omitempty" gorm:"column:s3_destination_id;type:text;index"`
	RemoteInstanceID  string                        `json:"remoteInstanceId,omitempty" gorm:"column:remote_instance_id;type:text"`
	S3DestinationName string                        `json:"s3DestinationName,omitempty" gorm:"-"`
	PolicyID          string                        `json:"policyId,omitempty" gorm:"column:policy_id;type:text;index"`
	Error             string                        `json:"error,omitempty" gorm:"column:error;type:text"`
	ActivityID        *string                       `json:"activityId,omitempty" gorm:"-"`
	Type              types.BackupManagementType    `json:"type" gorm:"-"`
	RemoteAvailable   *bool                         `json:"remoteAvailable,omitempty" gorm:"-"`
}

func (*VolumeBackup) TableName() string {
	return "volume_backups"
}

func (b *VolumeBackup) record() *types.VolumeBackup {
	return &types.VolumeBackup{
		ID: b.ID, UpdatedAt: b.UpdatedAt, VolumeName: b.VolumeName, Size: b.Size, CreatedAt: b.CreatedAt,
		Status: string(b.Status), Trigger: string(b.Trigger), Destination: b.Destination, Format: types.VolumeBackupFormat(b.Format),
		LocalSnapshotID: b.LocalSnapshotID, RemoteSnapshotID: b.RemoteSnapshotID, S3DestinationID: b.S3DestinationID,
		RemoteInstanceID: b.RemoteInstanceID, S3DestinationName: b.S3DestinationName, PolicyID: b.PolicyID, Error: b.Error,
		ActivityID: b.ActivityID, Type: b.Type, RemoteAvailable: b.RemoteAvailable,
	}
}

func volumeBackupFromRecord(record *types.VolumeBackup) *VolumeBackup {
	entry := &VolumeBackup{
		VolumeName: record.VolumeName, Size: record.Size, CreatedAt: record.CreatedAt, Status: VolumeBackupStatus(record.Status),
		Trigger: VolumeBackupTrigger(record.Trigger), Destination: record.Destination, Format: VolumeBackupFormat(record.Format),
		LocalSnapshotID: record.LocalSnapshotID, RemoteSnapshotID: record.RemoteSnapshotID, S3DestinationID: record.S3DestinationID,
		RemoteInstanceID: record.RemoteInstanceID, S3DestinationName: record.S3DestinationName, PolicyID: record.PolicyID,
		Error: record.Error, ActivityID: record.ActivityID, Type: record.Type, RemoteAvailable: record.RemoteAvailable,
	}
	entry.ID, entry.UpdatedAt = record.ID, record.UpdatedAt
	return entry
}
