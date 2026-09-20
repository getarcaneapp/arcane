package recovery

import (
	"time"

	"github.com/moby/moby/api/types/mount"
)

// ManifestFormatVersion is the manifest format new system backups write.
const ManifestFormatVersion = 2

// Manifest is the recovery manifest stored inside every system backup.
// Version 2 records where Arcane data and projects live in the snapshot.
type Manifest struct {
	FormatVersion int       `json:"formatVersion"`
	ArcaneVersion string    `json:"arcaneVersion"`
	BackupID      string    `json:"backupId,omitempty"`
	ActivityID    string    `json:"activityId,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	// DataPath and ProjectsPath are snapshot-relative roots (version 2).
	DataPath     string `json:"dataPath,omitempty"`
	ProjectsPath string `json:"projectsPath,omitempty"`
	// DatabasePath is the SQLite file relative to DataPath (version 2).
	DatabasePath string            `json:"databasePath,omitempty"`
	Environment  map[string]string `json:"environment"`
}

// RestoreTarget is a writable destination inside a helper container: the
// mounts to attach (the enclosing mount first, then mounts nested inside it)
// and the path beneath them that receives the restored files.
type RestoreTarget struct {
	Mounts []mount.Mount `json:"mounts"`
	Path   string        `json:"path"`
}

// RestoreRepository addresses the Rustic repository a stage reads from.
type RestoreRepository struct {
	Environment []string      `json:"environment"`
	Mounts      []mount.Mount `json:"mounts"`
}

// RestoreStage restores one snapshot path into one target.
type RestoreStage struct {
	Repository RestoreRepository `json:"repository"`
	SnapshotID string            `json:"snapshotId"`
	SourcePath string            `json:"sourcePath"`
	Target     RestoreTarget     `json:"target"`
}

// RestoreRequest is handed to the detached recovery helper.
type RestoreRequest struct {
	BackupID         string        `json:"backupId"`
	ContainerID      string        `json:"containerId"`
	ContainerImage   string        `json:"containerImage"`
	SnapshotID       string        `json:"snapshotId"`
	LocalSnapshotID  string        `json:"localSnapshotId,omitempty"`
	RemoteSnapshotID string        `json:"remoteSnapshotId,omitempty"`
	S3DestinationID  string        `json:"s3DestinationId,omitempty"`
	Size             int64         `json:"size,omitempty"`
	SafetyBackup     *SafetyBackup `json:"safetyBackup,omitempty"`
	RecoveryKey      string        `json:"recoveryKey"`
	// ProjectsSetting is the current projectsDirectory setting; it survives the
	// restore so projects keep resolving to where they were restored.
	ProjectsSetting string `json:"projectsSetting,omitempty"`
	// ProjectsIncluded is false for backups that predate separately mounted
	// projects; the current projects directory is then left untouched.
	ProjectsIncluded bool `json:"projectsIncluded"`
	// Stages restore the backup in order; RollbackStages restore the safety
	// snapshot when any stage fails.
	Stages         []RestoreStage `json:"stages"`
	RollbackStages []RestoreStage `json:"rollbackStages,omitempty"`
	// NetworkMode attaches the detached Rustic restore container to one of
	// Arcane's networks so network-local S3 endpoints stay reachable while
	// Arcane itself is stopped.
	NetworkMode string `json:"networkMode,omitempty"`
}

type SafetyBackup struct {
	ID              string    `json:"id"`
	LocalSnapshotID string    `json:"localSnapshotId"`
	Size            int64     `json:"size"`
	CreatedAt       time.Time `json:"createdAt"`
}
