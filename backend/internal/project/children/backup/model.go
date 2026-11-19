package backup

import (
	"time"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/types/v3"
)

type (
	ProjectBackup struct {
		database.BaseModel

		CompletedAt       *time.Time                    `gorm:"column:completed_at"`
		OrphanedAt        *time.Time                    `gorm:"column:orphaned_at"`
		ProjectID         string                        `gorm:"column:project_id;index"`
		ProjectName       string                        `gorm:"column:project_name"`
		ComposeName       string                        `gorm:"column:compose_name"`
		Status            string                        `gorm:"column:status"`
		Trigger           string                        `gorm:"column:trigger"`
		Destination       types.VolumeBackupDestination `gorm:"column:destination"`
		LocalSnapshotID   string                        `gorm:"column:local_snapshot_id"`
		RemoteSnapshotID  string                        `gorm:"column:remote_snapshot_id"`
		S3DestinationID   string                        `gorm:"column:s3_destination_id"`
		PolicyID          string                        `gorm:"column:policy_id;index"`
		Error             string                        `gorm:"column:error"`
		Volumes           []types.ProjectBackupVolume   `gorm:"column:volumes;serializer:json"`
		Size              int64                         `gorm:"column:size"`
		StoppedContainers bool                          `gorm:"column:stopped_containers"`
	}

	// ProjectBackupPolicy schedules backups of a project; nil Volumes selects the volumes a manual backup includes by default.
	ProjectBackupPolicy struct {
		database.BaseModel

		ProjectID       string   `gorm:"column:project_id;not null;index"`
		Schedule        string   `gorm:"column:schedule;not null"`
		S3DestinationID string   `gorm:"column:s3_destination_id"`
		Volumes         []string `gorm:"column:volumes;serializer:json"`
		RetentionCount  int      `gorm:"column:retention_count"`
		Enabled         bool     `gorm:"column:enabled"`
		StopContainers  bool     `gorm:"column:stop_containers"`
		LocalEnabled    bool     `gorm:"column:local_enabled"`
		S3Enabled       bool     `gorm:"column:s3_enabled"`
	}
)

func (ProjectBackup) TableName() string { return "project_backups" }

func (b *ProjectBackup) DTO() types.ProjectBackup {
	return types.ProjectBackup{
		CreatedAt:         b.CreatedAt,
		CompletedAt:       b.CompletedAt,
		ID:                b.ID,
		ProjectID:         b.ProjectID,
		ProjectName:       b.ProjectName,
		ComposeName:       b.ComposeName,
		Status:            b.Status,
		Trigger:           b.Trigger,
		Destination:       b.Destination,
		LocalSnapshotID:   b.LocalSnapshotID,
		RemoteSnapshotID:  b.RemoteSnapshotID,
		S3DestinationID:   b.S3DestinationID,
		PolicyID:          b.PolicyID,
		Error:             b.Error,
		Volumes:           b.Volumes,
		Size:              b.Size,
		StoppedContainers: b.StoppedContainers,
	}
}

func (ProjectBackupPolicy) TableName() string { return "project_backup_policies" }

func (p *ProjectBackupPolicy) DTO(lastRun *types.ProjectBackup, s3DestinationName string) types.ProjectBackupPolicy {
	return types.ProjectBackupPolicy{
		LastRun:           lastRun,
		ID:                p.ID,
		ProjectID:         p.ProjectID,
		Schedule:          p.Schedule,
		S3DestinationID:   p.S3DestinationID,
		S3DestinationName: s3DestinationName,
		Volumes:           p.Volumes,
		RetentionCount:    p.RetentionCount,
		Enabled:           p.Enabled,
		StopContainers:    p.StopContainers,
		LocalEnabled:      p.LocalEnabled,
		S3Enabled:         p.S3Enabled,
	}
}
