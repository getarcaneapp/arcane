package volume

type BackupEntry struct {
	ID              string `json:"id" doc:"Unique identifier of the backup"`
	VolumeName      string `json:"volumeName" doc:"Name of the volume"`
	Size            int64  `json:"size" doc:"Size of the backup archive in bytes"`
	CreatedAt       string `json:"createdAt" doc:"When the backup was created"`
	Status          string `json:"status" doc:"Backup result status"`
	Trigger         string `json:"trigger" doc:"How the backup was started"`
	RemoteKey       string `json:"remoteKey,omitempty" doc:"S3 object key when uploaded"`
	S3DestinationID string `json:"s3DestinationId,omitempty" doc:"S3 destination used by the backup"`
	Error           string `json:"error,omitempty" doc:"Backup error when the run failed"`
}

type BackupPolicy struct {
	VolumeName      string       `json:"volumeName"`
	Enabled         bool         `json:"enabled"`
	Schedule        string       `json:"schedule"`
	RetentionCount  int          `json:"retentionCount"`
	StopContainers  bool         `json:"stopContainers"`
	LocalEnabled    bool         `json:"localEnabled"`
	S3Enabled       bool         `json:"s3Enabled"`
	S3DestinationID string       `json:"s3DestinationId,omitempty"`
	S3Available     bool         `json:"s3Available"`
	S3Bucket        string       `json:"s3Bucket,omitempty"`
	LastRun         *BackupEntry `json:"lastRun,omitempty"`
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
