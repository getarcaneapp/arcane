package recovery

import (
	"time"

	"github.com/moby/moby/api/types/mount"
)

type RestoreRequest struct {
	BackupID              string        `json:"backupId"`
	ContainerID           string        `json:"containerId"`
	ContainerImage        string        `json:"containerImage"`
	SnapshotID            string        `json:"snapshotId"`
	SnapshotPath          string        `json:"snapshotPath"`
	LocalSnapshotID       string        `json:"localSnapshotId,omitempty"`
	RemoteSnapshotID      string        `json:"remoteSnapshotId,omitempty"`
	S3DestinationID       string        `json:"s3DestinationId,omitempty"`
	Size                  int64         `json:"size,omitempty"`
	SafetyBackup          *SafetyBackup `json:"safetyBackup,omitempty"`
	RecoveryKey           string        `json:"recoveryKey"`
	RepositoryEnvironment []string      `json:"repositoryEnvironment"`
	RepositoryMounts      []mount.Mount `json:"repositoryMounts"`
	AppDataMount          mount.Mount   `json:"appDataMount"`
}

type SafetyBackup struct {
	ID              string    `json:"id"`
	LocalSnapshotID string    `json:"localSnapshotId"`
	Size            int64     `json:"size"`
	CreatedAt       time.Time `json:"createdAt"`
}

type Manifest struct {
	FormatVersion int               `json:"formatVersion"`
	ArcaneVersion string            `json:"arcaneVersion"`
	BackupID      string            `json:"backupId,omitempty"`
	ActivityID    string            `json:"activityId,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	Environment   map[string]string `json:"environment"`
}
