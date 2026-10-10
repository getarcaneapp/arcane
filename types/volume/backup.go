package volume

import (
	"time"

	"github.com/getarcaneapp/arcane/types/v2/backup"
)

type BackupDestination string

const (
	BackupDestinationLocal   BackupDestination = "local"
	BackupDestinationS3      BackupDestination = "s3"
	BackupDestinationLocalS3 BackupDestination = "local_s3"

	BackupFormatArchive BackupFormat = "archive"
	BackupFormatRustic  BackupFormat = "rustic"
)

type CreateBackupRequest struct {
	Destination     BackupDestination `json:"destination,omitempty" doc:"Optional destination override for this manual backup"`
	PolicyID        string            `json:"policyId,omitempty" doc:"Optional backup policy whose settings should be used"`
	S3DestinationID string            `json:"s3DestinationId,omitempty" doc:"Saved S3 destination for a manual remote backup"`
}

type UploadBackupRequest struct {
	S3DestinationID string `json:"s3DestinationId" doc:"S3 destination for the uploaded backup"`
}

type DiscoverBackupsRequest struct {
	S3DestinationID string `json:"s3DestinationId" doc:"S3 destination to scan for existing volume backups"`
}

type DiscoverBackupsResponse struct {
	Count  int      `json:"count" doc:"Number of newly discovered volume backups"`
	Errors []string `json:"errors,omitempty" doc:"Per-repository failures encountered during discovery"`
}

type BackupFormat string

// Backup is one volume backup run.
type Backup struct {
	ID                string                `json:"id"`
	UpdatedAt         *time.Time            `json:"updatedAt,omitempty"`
	VolumeName        string                `json:"volumeName"`
	Size              int64                 `json:"size"`
	CreatedAt         time.Time             `json:"createdAt"`
	Status            string                `json:"status"`
	Trigger           string                `json:"trigger"`
	Destination       BackupDestination     `json:"destination"`
	Format            BackupFormat          `json:"format"`
	LocalSnapshotID   string                `json:"localSnapshotId,omitempty"`
	RemoteSnapshotID  string                `json:"remoteSnapshotId,omitempty"`
	S3DestinationID   string                `json:"s3DestinationId,omitempty"`
	RemoteInstanceID  string                `json:"remoteInstanceId,omitempty"`
	S3DestinationName string                `json:"s3DestinationName,omitempty"`
	PolicyID          string                `json:"policyId,omitempty"`
	Error             string                `json:"error,omitempty"`
	ActivityID        *string               `json:"activityId,omitempty"`
	Type              backup.ManagementType `json:"type"`
	RemoteAvailable   *bool                 `json:"remoteAvailable,omitempty"`
}

type BackupEntry struct {
	RemoteAvailable   *bool                 `json:"remoteAvailable,omitempty"`
	ActivityID        *string               `json:"activityId,omitempty"`
	ID                string                `json:"id" doc:"Unique identifier of the backup"`
	VolumeName        string                `json:"volumeName" doc:"Name of the volume"`
	Size              int64                 `json:"size" doc:"Total size of the backup contents"`
	CreatedAt         string                `json:"createdAt" doc:"When the backup was created"`
	Status            string                `json:"status" doc:"Backup result status"`
	Trigger           string                `json:"trigger" doc:"How the backup was started"`
	Destination       BackupDestination     `json:"destination" doc:"Requested backup storage target"`
	Format            BackupFormat          `json:"format" doc:"Storage format of the backup: legacy tar.gz archive or Rustic snapshot"`
	LocalSnapshotID   string                `json:"localSnapshotId,omitempty" doc:"Snapshot ID in the local Rustic repository"`
	RemoteSnapshotID  string                `json:"remoteSnapshotId,omitempty" doc:"Snapshot ID in the S3 Rustic repository"`
	S3DestinationID   string                `json:"s3DestinationId,omitempty" doc:"S3 destination used by the backup"`
	S3DestinationName string                `json:"s3DestinationName,omitempty" doc:"Name of the S3 destination used by the backup"`
	PolicyID          string                `json:"policyId,omitempty" doc:"Backup policy that created the backup"`
	Error             string                `json:"error,omitempty" doc:"Backup error when the run failed"`
	Type              backup.ManagementType `json:"type" doc:"Whether the backup was system-managed or volume-managed"`
}

// Entry is the backup as the API lists it.
func (b *Backup) Entry() BackupEntry {
	return BackupEntry{
		RemoteAvailable:   b.RemoteAvailable,
		ActivityID:        b.ActivityID,
		ID:                b.ID,
		VolumeName:        b.VolumeName,
		Size:              b.Size,
		CreatedAt:         b.CreatedAt.Format(time.RFC3339),
		Status:            b.Status,
		Trigger:           b.Trigger,
		Destination:       b.Destination,
		Format:            b.Format,
		LocalSnapshotID:   b.LocalSnapshotID,
		RemoteSnapshotID:  b.RemoteSnapshotID,
		S3DestinationID:   b.S3DestinationID,
		S3DestinationName: b.S3DestinationName,
		PolicyID:          b.PolicyID,
		Error:             b.Error,
		Type:              b.Type,
	}
}

type BackupPolicy struct {
	ID                string       `json:"id"`
	VolumeName        string       `json:"volumeName"`
	Enabled           bool         `json:"enabled"`
	Schedule          string       `json:"schedule"`
	RetentionCount    int          `json:"retentionCount"`
	StopContainers    bool         `json:"stopContainers"`
	LocalEnabled      bool         `json:"localEnabled"`
	S3Enabled         bool         `json:"s3Enabled"`
	S3DestinationID   string       `json:"s3DestinationId,omitempty"`
	S3DestinationName string       `json:"s3DestinationName,omitempty"`
	S3Available       bool         `json:"s3Available"`
	S3Bucket          string       `json:"s3Bucket,omitempty"`
	LastRun           *BackupEntry `json:"lastRun,omitempty"`
}

type UpdateBackupPolicy = backup.UpdateBackupPolicy

type BackupPolicyCollection struct {
	Policies    []BackupPolicy `json:"policies"`
	S3Available bool           `json:"s3Available"`
}

type UpdateBackupPolicies struct {
	Policies []UpdateBackupPolicy `json:"policies"`
}
