package volume

type BackupDestination string

const (
	BackupDestinationLocal   BackupDestination = "local"
	BackupDestinationS3      BackupDestination = "s3"
	BackupDestinationLocalS3 BackupDestination = "local_s3"
)

type CreateBackupRequest struct {
	Destination BackupDestination `json:"destination,omitempty" doc:"Optional destination override for this manual backup"`
}

type BackupEntry struct {
	ID                string            `json:"id" doc:"Unique identifier of the backup"`
	VolumeName        string            `json:"volumeName" doc:"Name of the volume"`
	Size              int64             `json:"size" doc:"Total size of files in the Rustic snapshot"`
	CreatedAt         string            `json:"createdAt" doc:"When the backup was created"`
	Status            string            `json:"status" doc:"Backup result status"`
	Trigger           string            `json:"trigger" doc:"How the backup was started"`
	Destination       BackupDestination `json:"destination" doc:"Requested backup storage target"`
	LocalSnapshotID   string            `json:"localSnapshotId,omitempty" doc:"Snapshot ID in the local Rustic repository"`
	RemoteSnapshotID  string            `json:"remoteSnapshotId,omitempty" doc:"Snapshot ID in the S3 Rustic repository"`
	S3DestinationID   string            `json:"s3DestinationId,omitempty" doc:"S3 destination used by the backup"`
	S3DestinationName string            `json:"s3DestinationName,omitempty" doc:"Name of the S3 destination used by the backup"`
	Error             string            `json:"error,omitempty" doc:"Backup error when the run failed"`
}

type BackupPolicy struct {
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

type UpdateBackupPolicy struct {
	Enabled         bool   `json:"enabled"`
	Schedule        string `json:"schedule"`
	RetentionCount  int    `json:"retentionCount"`
	StopContainers  bool   `json:"stopContainers"`
	LocalEnabled    bool   `json:"localEnabled"`
	S3Enabled       bool   `json:"s3Enabled"`
	S3DestinationID string `json:"s3DestinationId,omitempty"`
}
