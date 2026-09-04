package volume

import (
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/volume"
)

type VolumeBackupStatus string

const (
	VolumeBackupStatusRunning   VolumeBackupStatus = "running"
	VolumeBackupStatusSucceeded VolumeBackupStatus = "succeeded"
	VolumeBackupStatusFailed    VolumeBackupStatus = "failed"
)

type VolumeBackupTrigger string

const (
	VolumeBackupTriggerManual    VolumeBackupTrigger = "manual"
	VolumeBackupTriggerScheduled VolumeBackupTrigger = "scheduled"
	VolumeBackupTriggerSafety    VolumeBackupTrigger = "safety"
)

type VolumeBackupFormat string

const (
	VolumeBackupFormatArchive VolumeBackupFormat = "archive"
	VolumeBackupFormatRustic  VolumeBackupFormat = "rustic"
)

type VolumeBackup struct {
	database.BaseModel

	VolumeName        string                     `json:"volumeName" gorm:"column:volume_name;index"`
	Size              int64                      `json:"size" gorm:"column:size"`
	CreatedAt         time.Time                  `json:"createdAt" gorm:"column:created_at"`
	Status            VolumeBackupStatus         `json:"status" gorm:"column:status;type:text;not null;default:succeeded"`
	Trigger           VolumeBackupTrigger        `json:"trigger" gorm:"column:trigger;type:text;not null;default:manual"`
	Destination       volume.BackupDestination   `json:"destination" gorm:"column:destination;type:text;not null;default:local"`
	Format            VolumeBackupFormat         `json:"format" gorm:"column:format;type:text;not null;default:archive"`
	LocalSnapshotID   string                     `json:"localSnapshotId,omitempty" gorm:"column:local_snapshot_id;type:text"`
	RemoteSnapshotID  string                     `json:"remoteSnapshotId,omitempty" gorm:"column:remote_snapshot_id;type:text"`
	S3DestinationID   string                     `json:"s3DestinationId,omitempty" gorm:"column:s3_destination_id;type:text;index"`
	S3DestinationName string                     `json:"s3DestinationName,omitempty" gorm:"-"`
	PolicyID          string                     `json:"policyId,omitempty" gorm:"column:policy_id;type:text;index"`
	Error             string                     `json:"error,omitempty" gorm:"column:error;type:text"`
	ActivityID        *string                    `json:"activityId,omitempty" gorm:"-"`
	Type              backuptypes.ManagementType `json:"type" gorm:"-"`
}

func (*VolumeBackup) TableName() string {
	return "volume_backups"
}

func (b *VolumeBackup) ToDTO() volume.BackupEntry {
	return volume.BackupEntry{
		ActivityID:        b.ActivityID,
		ID:                b.ID,
		VolumeName:        b.VolumeName,
		Size:              b.Size,
		CreatedAt:         b.CreatedAt.Format(time.RFC3339),
		Status:            string(b.Status),
		Trigger:           string(b.Trigger),
		Destination:       b.Destination,
		Format:            volume.BackupFormat(b.Format),
		LocalSnapshotID:   b.LocalSnapshotID,
		RemoteSnapshotID:  b.RemoteSnapshotID,
		S3DestinationID:   b.S3DestinationID,
		S3DestinationName: b.S3DestinationName,
		PolicyID:          b.PolicyID,
		Error:             b.Error,
		Type:              b.Type,
	}
}

type VolumeBackupPolicy struct {
	database.BaseModel

	VolumeName      string `json:"volumeName" gorm:"column:volume_name;not null;index"`
	Enabled         bool   `json:"enabled" gorm:"column:enabled;not null;default:false"`
	Schedule        string `json:"schedule" gorm:"column:schedule;type:text;not null"`
	RetentionCount  int    `json:"retentionCount" gorm:"column:retention_count;not null;default:7"`
	StopContainers  bool   `json:"stopContainers" gorm:"column:stop_containers;not null;default:false"`
	LocalEnabled    bool   `json:"localEnabled" gorm:"column:local_enabled;not null"`
	S3Enabled       bool   `json:"s3Enabled" gorm:"column:s3_enabled;not null;default:false"`
	S3DestinationID string `json:"s3DestinationId,omitempty" gorm:"column:s3_destination_id;type:text;index"`
}

func (VolumeBackupPolicy) TableName() string {
	return "volume_backup_policies"
}

func (p VolumeBackupPolicy) ToDTO(lastRun *VolumeBackup) volume.BackupPolicy {
	dto := volume.BackupPolicy{
		ID:              p.ID,
		VolumeName:      p.VolumeName,
		Enabled:         p.Enabled,
		Schedule:        p.Schedule,
		RetentionCount:  p.RetentionCount,
		StopContainers:  p.StopContainers,
		LocalEnabled:    p.LocalEnabled,
		S3Enabled:       p.S3Enabled,
		S3DestinationID: p.S3DestinationID,
	}
	if lastRun != nil {
		lastRunDTO := lastRun.ToDTO()
		dto.LastRun = &lastRunDTO
	}
	return dto
}
