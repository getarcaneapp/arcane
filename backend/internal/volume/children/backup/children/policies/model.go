package policies

import (
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

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

func (p VolumeBackupPolicy) ToDTO(lastRun *types.VolumeBackupEntry) types.VolumeBackupPolicy {
	return types.VolumeBackupPolicy{
		ID:              p.ID,
		VolumeName:      p.VolumeName,
		Enabled:         p.Enabled,
		Schedule:        p.Schedule,
		RetentionCount:  p.RetentionCount,
		StopContainers:  p.StopContainers,
		LocalEnabled:    p.LocalEnabled,
		S3Enabled:       p.S3Enabled,
		S3DestinationID: p.S3DestinationID,
		LastRun:         lastRun,
	}
}
