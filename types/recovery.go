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
		// ExcludedProjectRoots are the project folders, relative to DataPath, a backup without projects left out.
		ExcludedProjectRoots []string `json:"excludedProjectRoots,omitempty"`
		FormatVersion        int      `json:"formatVersion"`
		ProjectsExcluded     bool     `json:"projectsExcluded,omitempty"`
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
		// Swap restores into a staging folder inside the data folder and then swaps it in.
		Swap       *RecoveryRestoreSwap      `json:"swap,omitempty"`
		Target     RecoveryRestoreTarget     `json:"target"`
		SnapshotID string                    `json:"snapshotId"`
		SourcePath string                    `json:"sourcePath"`
		Globs      []string                  `json:"globs,omitempty"`
		Repository RecoveryRestoreRepository `json:"repository"`
	}

	RecoveryRestoreSwap struct {
		// Directory is the live data folder as the recovery helper sees it.
		Directory string `json:"directory"`
		ID        string `json:"id"`
		// Preserve lists paths relative to Directory that the swap never enters or changes.
		Preserve []string `json:"preserve"`
	}

	RecoveryRestoreRequest struct {
		SafetyBackup     *RecoverySafetyBackup `json:"safetyBackup,omitempty"`
		BackupID         string                `json:"backupId"`
		ContainerID      string                `json:"containerId"`
		ContainerImage   string                `json:"containerImage"`
		SnapshotID       string                `json:"snapshotId"`
		LocalSnapshotID  string                `json:"localSnapshotId,omitempty"`
		RemoteSnapshotID string                `json:"remoteSnapshotId,omitempty"`
		S3DestinationID  string                `json:"s3DestinationId,omitempty"`
		RecoveryKey      string                `json:"recoveryKey"`
		ProjectsSetting  string                `json:"projectsSetting,omitempty"`
		// ProjectsDirectoriesSetting is the current projectsDirectories value the restored instance keeps.
		ProjectsDirectoriesSetting string                 `json:"projectsDirectoriesSetting,omitempty"`
		NetworkMode                string                 `json:"networkMode,omitempty"`
		Stages                     []RecoveryRestoreStage `json:"stages"`
		RollbackStages             []RecoveryRestoreStage `json:"rollbackStages,omitempty"`
		Size                       int64                  `json:"size,omitempty"`
		ProjectsIncluded           bool                   `json:"projectsIncluded"`
	}

	RecoverySafetyBackup struct {
		CreatedAt       time.Time `json:"createdAt"`
		ID              string    `json:"id"`
		LocalSnapshotID string    `json:"localSnapshotId"`
		Size            int64     `json:"size"`
	}
)

const (
	// RecoveryManifestFormatVersion is written by system backups that leave projects out.
	RecoveryManifestFormatVersion = 4
	// RecoveryManifestProjectsFormatVersion is written by safety backups that include projects.
	RecoveryManifestProjectsFormatVersion = 3
)
