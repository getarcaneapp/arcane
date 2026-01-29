package models

import (
	"time"

	"github.com/getarcaneapp/arcane/types/volume"
)

type VolumeBackup struct {
	BaseModel
	VolumeName string    `json:"volumeName" gorm:"column:volume_name;index"`
	Size       int64     `json:"size" gorm:"column:size"`
	CreatedAt  time.Time `json:"createdAt" gorm:"column:created_at"`
}

func (*VolumeBackup) TableName() string {
	return "volume_backups"
}

func (b *VolumeBackup) ToDTO() volume.BackupEntry {
	return volume.BackupEntry{
		ID:         b.ID,
		VolumeName: b.VolumeName,
		Size:       b.Size,
		CreatedAt:  b.CreatedAt.Format(time.RFC3339),
	}
}
