package types

import (
	"time"

	"github.com/moby/moby/api/types/mount"
)

type (
	RecoveryManifest struct {
		CreatedAt     time.Time         `json:"createdAt"`
		Environment   map[string]string `json:"environment"`
		ArcaneVersion string            `json:"arcaneVersion"`
		BackupID      string            `json:"backupId,omitempty"`
		ActivityID    string            `json:"activityId,omitempty"`
		DataPath      string            `json:"dataPath,omitempty"`
		ProjectsPath  string            `json:"projectsPath,omitempty"`
		DatabasePath  string            `json:"databasePath,omitempty"`
		FormatVersion int               `json:"formatVersion"`
	}

	RecoveryRestoreTarget struct {
		Path   string        `json:"path"`
		Mounts []mount.Mount `json:"mounts"`
	}

	RecoveryRestoreRepository struct {
		Environment []string      `json:"environment"`
		Mounts      []mount.Mount `json:"mounts"`
	}

	RecoveryRestoreStage struct {
		Target     RecoveryRestoreTarget     `json:"target"`
		SnapshotID string                    `json:"snapshotId"`
		SourcePath string                    `json:"sourcePath"`
		Repository RecoveryRestoreRepository `json:"repository"`
	}

	RecoveryRestoreRequest struct {
		SafetyBackup     *RecoverySafetyBackup  `json:"safetyBackup,omitempty"`
		BackupID         string                 `json:"backupId"`
		ContainerID      string                 `json:"containerId"`
		ContainerImage   string                 `json:"containerImage"`
		SnapshotID       string                 `json:"snapshotId"`
		LocalSnapshotID  string                 `json:"localSnapshotId,omitempty"`
		RemoteSnapshotID string                 `json:"remoteSnapshotId,omitempty"`
		S3DestinationID  string                 `json:"s3DestinationId,omitempty"`
		RecoveryKey      string                 `json:"recoveryKey"`
		ProjectsSetting  string                 `json:"projectsSetting,omitempty"`
		NetworkMode      string                 `json:"networkMode,omitempty"`
		Stages           []RecoveryRestoreStage `json:"stages"`
		RollbackStages   []RecoveryRestoreStage `json:"rollbackStages,omitempty"`
		Size             int64                  `json:"size,omitempty"`
		ProjectsIncluded bool                   `json:"projectsIncluded"`
	}

	RecoverySafetyBackup struct {
		CreatedAt       time.Time `json:"createdAt"`
		ID              string    `json:"id"`
		LocalSnapshotID string    `json:"localSnapshotId"`
		Size            int64     `json:"size"`
	}
)

const (
	RecoveryManifestFormatVersion = 3
)
