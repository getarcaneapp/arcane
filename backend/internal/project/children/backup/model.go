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
		ProjectID         string                        `gorm:"column:project_id;index"`
		ProjectName       string                        `gorm:"column:project_name"`
		ComposeName       string                        `gorm:"column:compose_name"`
		Status            string                        `gorm:"column:status"`
		Trigger           string                        `gorm:"column:trigger"`
		Destination       types.VolumeBackupDestination `gorm:"column:destination"`
		LocalSnapshotID   string                        `gorm:"column:local_snapshot_id"`
		RemoteSnapshotID  string                        `gorm:"column:remote_snapshot_id"`
		S3DestinationID   string                        `gorm:"column:s3_destination_id"`
		Error             string                        `gorm:"column:error"`
		Volumes           []types.ProjectBackupVolume   `gorm:"column:volumes;serializer:json"`
		Size              int64                         `gorm:"column:size"`
		StoppedContainers bool                          `gorm:"column:stopped_containers"`
	}

	// DTOOptions carries what a backup's API value shows but its row doesn't store.
	DTOOptions struct {
		ActivityID        *string
		S3DestinationName string
	}
)

func (ProjectBackup) TableName() string { return "project_backups" }

func (b *ProjectBackup) DTO(opts DTOOptions) types.ProjectBackup {
	return types.ProjectBackup{
		ActivityID:        opts.ActivityID,
		S3DestinationName: opts.S3DestinationName,
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
		Error:             b.Error,
		Volumes:           b.Volumes,
		Size:              b.Size,
		StoppedContainers: b.StoppedContainers,
	}
}
