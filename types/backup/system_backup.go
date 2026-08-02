package backup

import "time"

type SystemBackupDestination string

const (
	SystemBackupDestinationLocal   SystemBackupDestination = "local"
	SystemBackupDestinationS3      SystemBackupDestination = "s3"
	SystemBackupDestinationLocalS3 SystemBackupDestination = "local_s3"
)

type SystemBackupRun struct {
	ID                string                  `json:"id"`
	Size              int64                   `json:"size"`
	CreatedAt         time.Time               `json:"createdAt"`
	Status            string                  `json:"status"`
	Trigger           string                  `json:"trigger"`
	Destination       SystemBackupDestination `json:"destination"`
	LocalSnapshotID   string                  `json:"localSnapshotId,omitempty"`
	RemoteSnapshotID  string                  `json:"remoteSnapshotId,omitempty"`
	S3DestinationID   string                  `json:"s3DestinationId,omitempty"`
	S3DestinationName string                  `json:"s3DestinationName,omitempty"`
	PolicyID          string                  `json:"policyId,omitempty"`
	Error             string                  `json:"error,omitempty"`
}

type SystemBackupPolicy struct {
	ID                string           `json:"id"`
	Enabled           bool             `json:"enabled"`
	Schedule          string           `json:"schedule"`
	RetentionCount    int              `json:"retentionCount"`
	LocalEnabled      bool             `json:"localEnabled"`
	S3Enabled         bool             `json:"s3Enabled"`
	S3DestinationID   string           `json:"s3DestinationId,omitempty"`
	S3DestinationName string           `json:"s3DestinationName,omitempty"`
	LastRun           *SystemBackupRun `json:"lastRun,omitempty"`
}

type UpdateSystemBackupPolicy struct {
	ID              string `json:"id,omitempty"`
	Enabled         bool   `json:"enabled"`
	Schedule        string `json:"schedule"`
	RetentionCount  int    `json:"retentionCount"`
	LocalEnabled    bool   `json:"localEnabled"`
	S3Enabled       bool   `json:"s3Enabled"`
	S3DestinationID string `json:"s3DestinationId,omitempty"`
}

type SystemBackupPolicyCollection struct {
	Policies          []SystemBackupPolicy `json:"policies"`
	RecoveryKeyStored bool                 `json:"recoveryKeyStored"`
}

type UpdateSystemBackupPolicies struct {
	Policies []UpdateSystemBackupPolicy `json:"policies"`
}

type SystemBackupRecoveryKeyStatus struct {
	Configured bool `json:"configured"`
}

type SystemBackupRecoveryKey struct {
	RecoveryKey string `json:"recoveryKey"`
}

type CreateSystemBackupRequest struct {
	Destination     SystemBackupDestination `json:"destination,omitempty"`
	S3DestinationID string                  `json:"s3DestinationId,omitempty"`
	RecoveryKey     string                  `json:"recoveryKey,omitempty"`
	PolicyID        string                  `json:"policyId,omitempty"`
}

type RestoreSystemBackupRequest struct {
	RecoveryKey string `json:"recoveryKey"`
}

type UploadSystemBackupRequest struct {
	S3DestinationID string `json:"s3DestinationId"`
	RecoveryKey     string `json:"recoveryKey"`
}

type DeleteSystemBackupRequest struct {
	RecoveryKey string `json:"recoveryKey,omitempty"`
}

type DiscoverSystemBackupsRequest struct {
	S3DestinationID string `json:"s3DestinationId"`
	RecoveryKey     string `json:"recoveryKey"`
}
