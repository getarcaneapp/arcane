package models

import (
	"time"

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

type VolumeBackup struct {
	BaseModel

	VolumeName      string              `json:"volumeName" gorm:"column:volume_name;index"`
	Size            int64               `json:"size" gorm:"column:size"`
	CreatedAt       time.Time           `json:"createdAt" gorm:"column:created_at"`
	Status          VolumeBackupStatus  `json:"status" gorm:"column:status;type:text;not null;default:succeeded"`
	Trigger         VolumeBackupTrigger `json:"trigger" gorm:"column:trigger;type:text;not null;default:manual"`
	RemoteKey       string              `json:"remoteKey,omitempty" gorm:"column:remote_key;type:text"`
	S3DestinationID string              `json:"s3DestinationId,omitempty" gorm:"column:s3_destination_id;type:text;index"`
	Error           string              `json:"error,omitempty" gorm:"column:error;type:text"`
	ActivityID      *string             `json:"activityId,omitempty" gorm:"-"`
}

func (*VolumeBackup) TableName() string {
	return "volume_backups"
}

func (b *VolumeBackup) ToDTO() volume.BackupEntry {
	return volume.BackupEntry{
		ID:              b.ID,
		VolumeName:      b.VolumeName,
		Size:            b.Size,
		CreatedAt:       b.CreatedAt.Format(time.RFC3339),
		Status:          string(b.Status),
		Trigger:         string(b.Trigger),
		RemoteKey:       b.RemoteKey,
		S3DestinationID: b.S3DestinationID,
		Error:           b.Error,
	}
}

type VolumeBackupPolicy struct {
	BaseModel

	VolumeName      string `json:"volumeName" gorm:"column:volume_name;not null;uniqueIndex"`
	Enabled         bool   `json:"enabled" gorm:"column:enabled;not null;default:false"`
	Schedule        string `json:"schedule" gorm:"column:schedule;type:text;not null"`
	RetentionCount  int    `json:"retentionCount" gorm:"column:retention_count;not null;default:7"`
	LocalEnabled    bool   `json:"localEnabled" gorm:"column:local_enabled;not null"`
	S3Enabled       bool   `json:"s3Enabled" gorm:"column:s3_enabled;not null;default:false"`
	S3DestinationID string `json:"s3DestinationId,omitempty" gorm:"column:s3_destination_id;type:text;index"`
}

func (VolumeBackupPolicy) TableName() string {
	return "volume_backup_policies"
}

func (p *VolumeBackupPolicy) ToDTO(lastRun *VolumeBackup) volume.BackupPolicy {
	dto := volume.BackupPolicy{
		VolumeName:      p.VolumeName,
		Enabled:         p.Enabled,
		Schedule:        p.Schedule,
		RetentionCount:  p.RetentionCount,
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
