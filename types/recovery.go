package types

import (
	"time"

	"github.com/moby/moby/api/types/mount"
)

type (
	// RecoveryManifest is the recovery manifest stored inside every system backup.
	// Version 2 records where Arcane data and projects live in the snapshot.
	RecoveryManifest struct {
		CreatedAt     time.Time         `json:"createdAt"`
		Environment   map[string]string `json:"environment"`
		ArcaneVersion string            `json:"arcaneVersion"`
		BackupID      string            `json:"backupId,omitempty"`
		ActivityID    string            `json:"activityId,omitempty"`
		// DataPath and ProjectsPath are snapshot-relative roots (version 2).
		DataPath     string `json:"dataPath,omitempty"`
		ProjectsPath string `json:"projectsPath,omitempty"`
		// DatabasePath is the SQLite file relative to DataPath (version 2).
		DatabasePath  string `json:"databasePath,omitempty"`
		FormatVersion int    `json:"formatVersion"`
	}

	// RecoveryRestoreTarget is a writable destination inside a helper container: the
	// mounts to attach (the enclosing mount first, then mounts nested inside it)
	// and the path beneath them that receives the restored files.
	RecoveryRestoreTarget struct {
		Path   string        `json:"path"`
		Mounts []mount.Mount `json:"mounts"`
	}

	// RecoveryRestoreRepository addresses the Rustic repository a stage reads from.
	RecoveryRestoreRepository struct {
		Environment []string      `json:"environment"`
		Mounts      []mount.Mount `json:"mounts"`
	}

	// RecoveryRestoreStage restores one snapshot path into one target.
	RecoveryRestoreStage struct {
		Target     RecoveryRestoreTarget     `json:"target"`
		SnapshotID string                    `json:"snapshotId"`
		SourcePath string                    `json:"sourcePath"`
		Repository RecoveryRestoreRepository `json:"repository"`
	}

	// RecoveryRestoreRequest is handed to the detached recovery helper.
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
		// ProjectsSetting is the current projectsDirectory setting; it survives the
		// restore so projects keep resolving to where they were restored.
		ProjectsSetting string `json:"projectsSetting,omitempty"`
		// NetworkMode attaches the detached Rustic restore container to one of
		// Arcane's networks so network-local S3 endpoints stay reachable while
		// Arcane itself is stopped.
		NetworkMode string `json:"networkMode,omitempty"`
		// Stages restore the backup in order; RollbackStages restore the safety
		// snapshot when any stage fails.
		Stages         []RecoveryRestoreStage `json:"stages"`
		RollbackStages []RecoveryRestoreStage `json:"rollbackStages,omitempty"`
		Size           int64                  `json:"size,omitempty"`
		// ProjectsIncluded is false for backups that predate separately mounted
		// projects; the current projects directory is then left untouched.
		ProjectsIncluded bool `json:"projectsIncluded"`
	}

	RecoverySafetyBackup struct {
		CreatedAt       time.Time `json:"createdAt"`
		ID              string    `json:"id"`
		LocalSnapshotID string    `json:"localSnapshotId"`
		Size            int64     `json:"size"`
	}
)

const (
	// RecoveryManifestFormatVersion is the manifest format new system backups write.
	RecoveryManifestFormatVersion = 3
)
