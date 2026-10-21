package types

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

// Volume represents a Docker volume.
type Volume struct {
	EnvironmentTag

	// ID is the unique identifier of the volume.
	//
	// Required: true
	ID string `json:"id" readOnly:"true"`

	// Name of the volume.
	//
	// Required: false
	Name string `json:"name" required:"false"`

	// Driver is the volume driver used.
	//
	// Required: false
	Driver string `json:"driver" required:"false"`

	// Mountpoint is the path where the volume is mounted.
	//
	// Required: true
	Mountpoint string `json:"mountpoint" readOnly:"true"`

	// Scope of the volume (local, global, etc).
	//
	// Required: true
	Scope string `json:"scope" readOnly:"true"`

	// Options contains driver-specific options.
	//
	// Required: false
	Options map[string]string `json:"options" required:"false"`

	// Labels contains user-defined metadata for the volume.
	//
	// Required: false
	Labels map[string]string `json:"labels" required:"false"`

	// CreatedAt is the time when the volume was created.
	//
	// Required: true
	CreatedAt string `json:"createdAt" readOnly:"true"`

	// InUse indicates if the volume is currently in use by a container.
	//
	// Required: true
	InUse bool `json:"inUse" readOnly:"true"`

	// UsageData contains size and reference count information.
	//
	// Required: false
	UsageData *volume.UsageData `json:"usageData,omitempty" readOnly:"true"`

	// Size is the size of the volume in bytes.
	//
	// Required: false
	Size int64 `json:"size" readOnly:"true"`

	// Containers is a list of container IDs using this volume.
	//
	// Required: true
	Containers []string `json:"containers" readOnly:"true"`

	// ActivityID is the activity created by a mutating volume action.
	//
	// Required: false
	ActivityID *string `json:"activityId,omitempty" readOnly:"true"`
}

// VolumeUsageCounts contains counts of volumes by usage status.
type VolumeUsageCounts struct {
	// Inuse is the number of volumes currently in use.
	//
	// Required: true
	Inuse int `json:"inuse"`

	// Unused is the number of volumes not in use.
	//
	// Required: true
	Unused int `json:"unused"`

	// Total is the total number of volumes.
	//
	// Required: true
	Total int `json:"total"`
}

// VolumePruneReport is the result of a volume prune operation.
type VolumePruneReport struct {
	// VolumesDeleted is a list of volume names that were deleted.
	//
	// Required: true
	VolumesDeleted []string `json:"volumesDeleted"`

	// SpaceReclaimed is the amount of space reclaimed in bytes.
	//
	// Required: true
	SpaceReclaimed uint64 `json:"spaceReclaimed"`

	// ActivityID is the activity created by the prune action.
	//
	// Required: false
	ActivityID *string `json:"activityId,omitempty"`
}

// VolumeRename is used to rename an existing volume.
type VolumeRename struct {
	// Name is the new volume name.
	//
	// Required: true
	Name string `json:"name" minLength:"1" doc:"New volume name"`
}

// NewVolume creates a Volume from a docker volume.Volume, calculating InUse
// based on whether the volume has a reference count of 1 or more.
//
// InUse is set to true if the volume's UsageData.RefCount is >= 1, false otherwise.
func NewVolume(v volume.Volume) Volume {
	mountpoint := v.Mountpoint
	if v.Options["type"] == "none" && v.Options["device"] != "" {
		mountpoint = v.Options["device"]
	}

	dto := Volume{
		ID:         v.Name,
		Name:       v.Name,
		Driver:     v.Driver,
		Mountpoint: mountpoint,
		Scope:      v.Scope,
		Options:    v.Options,
		Labels:     v.Labels,
		CreatedAt:  v.CreatedAt,
		Containers: make([]string, 0),
	}

	if v.UsageData != nil {
		dto.InUse = v.UsageData.RefCount >= 1
		dto.UsageData = v.UsageData
		dto.Size = v.UsageData.Size
	}

	return dto
}

type VolumeBackupDestination string

const (
	VolumeBackupDestinationLocal   VolumeBackupDestination = "local"
	VolumeBackupDestinationS3      VolumeBackupDestination = "s3"
	VolumeBackupDestinationLocalS3 VolumeBackupDestination = "local_s3"

	VolumeBackupFormatArchive VolumeBackupFormat = "archive"
	VolumeBackupFormatRustic  VolumeBackupFormat = "rustic"
)

type CreateVolumeBackupRequest struct {
	Destination     VolumeBackupDestination `json:"destination,omitempty" doc:"Optional destination override for this manual backup"`
	PolicyID        string                  `json:"policyId,omitempty" doc:"Optional backup policy whose settings should be used"`
	S3DestinationID string                  `json:"s3DestinationId,omitempty" doc:"Saved S3 destination for a manual remote backup"`
}

type UploadVolumeBackupRequest struct {
	S3DestinationID string `json:"s3DestinationId" doc:"S3 destination for the uploaded backup"`
}

type DiscoverVolumeBackupsRequest struct {
	S3DestinationID string `json:"s3DestinationId" doc:"S3 destination to scan for existing volume backups"`
}

type DiscoverVolumeBackupsResponse struct {
	Count  int      `json:"count" doc:"Number of newly discovered volume backups"`
	Errors []string `json:"errors,omitempty" doc:"Per-repository failures encountered during discovery"`
}

type VolumeBackupFormat string

// VolumeBackup is one volume backup run.
type VolumeBackup struct {
	ID                string                  `json:"id"`
	UpdatedAt         *time.Time              `json:"updatedAt,omitempty"`
	VolumeName        string                  `json:"volumeName"`
	Size              int64                   `json:"size"`
	CreatedAt         time.Time               `json:"createdAt"`
	Status            string                  `json:"status"`
	Trigger           string                  `json:"trigger"`
	Destination       VolumeBackupDestination `json:"destination"`
	Format            VolumeBackupFormat      `json:"format"`
	LocalSnapshotID   string                  `json:"localSnapshotId,omitempty"`
	RemoteSnapshotID  string                  `json:"remoteSnapshotId,omitempty"`
	S3DestinationID   string                  `json:"s3DestinationId,omitempty"`
	RemoteInstanceID  string                  `json:"remoteInstanceId,omitempty"`
	S3DestinationName string                  `json:"s3DestinationName,omitempty"`
	PolicyID          string                  `json:"policyId,omitempty"`
	Error             string                  `json:"error,omitempty"`
	ActivityID        *string                 `json:"activityId,omitempty"`
	Type              BackupManagementType    `json:"type"`
	RemoteAvailable   *bool                   `json:"remoteAvailable,omitempty"`
}

type VolumeBackupEntry struct {
	RemoteAvailable   *bool                   `json:"remoteAvailable,omitempty"`
	ActivityID        *string                 `json:"activityId,omitempty"`
	ID                string                  `json:"id" doc:"Unique identifier of the backup"`
	VolumeName        string                  `json:"volumeName" doc:"Name of the volume"`
	Size              int64                   `json:"size" doc:"Total size of the backup contents"`
	CreatedAt         string                  `json:"createdAt" doc:"When the backup was created"`
	Status            string                  `json:"status" doc:"Backup result status"`
	Trigger           string                  `json:"trigger" doc:"How the backup was started"`
	Destination       VolumeBackupDestination `json:"destination" doc:"Requested backup storage target"`
	Format            VolumeBackupFormat      `json:"format" doc:"Storage format of the backup: legacy tar.gz archive or Rustic snapshot"`
	LocalSnapshotID   string                  `json:"localSnapshotId,omitempty" doc:"Snapshot ID in the local Rustic repository"`
	RemoteSnapshotID  string                  `json:"remoteSnapshotId,omitempty" doc:"Snapshot ID in the S3 Rustic repository"`
	S3DestinationID   string                  `json:"s3DestinationId,omitempty" doc:"S3 destination used by the backup"`
	S3DestinationName string                  `json:"s3DestinationName,omitempty" doc:"Name of the S3 destination used by the backup"`
	PolicyID          string                  `json:"policyId,omitempty" doc:"Backup policy that created the backup"`
	Error             string                  `json:"error,omitempty" doc:"Backup error when the run failed"`
	Type              BackupManagementType    `json:"type" doc:"Whether the backup was system-managed or volume-managed"`
}

// Entry is the backup as the API lists it.
func (b *VolumeBackup) Entry() VolumeBackupEntry {
	return VolumeBackupEntry{
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

type VolumeBackupPolicy struct {
	ID                string             `json:"id"`
	VolumeName        string             `json:"volumeName"`
	Enabled           bool               `json:"enabled"`
	Schedule          string             `json:"schedule"`
	RetentionCount    int                `json:"retentionCount"`
	StopContainers    bool               `json:"stopContainers"`
	LocalEnabled      bool               `json:"localEnabled"`
	S3Enabled         bool               `json:"s3Enabled"`
	S3DestinationID   string             `json:"s3DestinationId,omitempty"`
	S3DestinationName string             `json:"s3DestinationName,omitempty"`
	S3Available       bool               `json:"s3Available"`
	S3Bucket          string             `json:"s3Bucket,omitempty"`
	LastRun           *VolumeBackupEntry `json:"lastRun,omitempty"`
}

type VolumeBackupPolicyCollection struct {
	Policies    []VolumeBackupPolicy `json:"policies"`
	S3Available bool                 `json:"s3Available"`
}

type UpdateVolumeBackupPolicies struct {
	Policies []UpdateBackupPolicy `json:"policies"`
}

// VolumeRenameEntry describes a source volume and the target configuration for a copy-based rename.
type VolumeRenameEntry struct {
	Key           string
	OldName       string
	NewName       string
	OldVolume     volume.Volume
	CreateOptions client.VolumeCreateOptions
}

type VolumeMigration interface {
	Apply(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type VolumeRenameCommitter interface {
	Commit(ctx context.Context) error
}

type VolumeJournalSource interface {
	JournalVolumes() []VolumeJournalVolume
}

type VolumeJournalVolume struct {
	Key     string            `json:"key"`
	OldName string            `json:"oldName"`
	NewName string            `json:"newName"`
	Driver  string            `json:"driver,omitempty"`
	Options map[string]string `json:"options,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}

type ProjectVolumeRenameConflictError struct {
	VolumeName string
}

type ProjectVolumeRenameInUseError struct {
	VolumeName   string
	ContainerIDs []string
}

type ProjectVolumeRenameInsufficientSpaceError struct {
	SourceVolume string
	TargetVolume string
	Detail       string
}

type TargetMissingWithSourceError struct {
	SourceVolume string
	TargetVolume string
}

type SourceCleanupError struct {
	SourceVolume string
	Err          error
}

type TargetPreservedDuringRollbackError struct {
	SourceVolume string
	TargetVolume string
	Err          error
}

type VolumesExternallyRemovedError struct {
	Volumes []VolumeJournalVolume
}

func (e *VolumesExternallyRemovedError) Error() string {
	if e == nil || len(e.Volumes) == 0 {
		return "committed project rename source and target volumes are both missing"
	}
	if len(e.Volumes) == 1 {
		vol := e.Volumes[0]
		return fmt.Sprintf("committed project rename target volume %s is missing and source volume %s is also missing", vol.NewName, vol.OldName)
	}
	return fmt.Sprintf("committed project rename source and target volumes are both missing for %d volume pairs", len(e.Volumes))
}

func (e *TargetPreservedDuringRollbackError) Unwrap() error {
	return e.Err
}

func (e *TargetPreservedDuringRollbackError) Error() string {
	return fmt.Sprintf("preserved project rename target volume %s during rollback to avoid data loss; source volume %s was not safe to rely on: %v", e.TargetVolume, e.SourceVolume, e.Err)
}

func (e *SourceCleanupError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *SourceCleanupError) Error() string {
	if e == nil {
		return "clean up committed project rename source volume"
	}
	if strings.TrimSpace(e.SourceVolume) == "" {
		return fmt.Sprintf("clean up committed project rename source volume: %v", e.Err)
	}
	return fmt.Sprintf("clean up committed project rename source volume %s: %v", e.SourceVolume, e.Err)
}

func (e *TargetMissingWithSourceError) Error() string {
	return fmt.Sprintf("committed project rename target volume %s is missing while source volume %s still exists", e.TargetVolume, e.SourceVolume)
}

func (e *ProjectVolumeRenameInsufficientSpaceError) Error() string {
	msg := "insufficient disk space to rename project volume"
	if e.SourceVolume != "" && e.TargetVolume != "" {
		msg = fmt.Sprintf("insufficient disk space to copy volume %s to %s", e.SourceVolume, e.TargetVolume)
	}
	if strings.TrimSpace(e.Detail) != "" {
		msg += ": " + strings.TrimSpace(e.Detail)
	}
	return msg
}

func (e *ProjectVolumeRenameInUseError) Error() string {
	if strings.TrimSpace(e.VolumeName) == "" {
		return "source volume is still attached to containers"
	}
	if len(e.ContainerIDs) == 0 {
		return "source volume is still attached to containers: " + e.VolumeName
	}
	return fmt.Sprintf("source volume is still attached to %d container(s): %s", len(e.ContainerIDs), e.VolumeName)
}

func (e *ProjectVolumeRenameConflictError) Error() string {
	if strings.TrimSpace(e.VolumeName) == "" {
		return "target volume already exists"
	}
	return "target volume already exists: " + e.VolumeName
}

const (
	FileOpRestoreFile = "restore_file"
)

type VolumeWorkspaceFileChange struct {
	Operation     string `json:"operation" binding:"required" enum:"create_file,create_folder,update_file,rename,move,delete,restore_file"`
	RelativePath  string `json:"relativePath" binding:"required"`
	NewName       string `json:"newName,omitempty"`
	NewParentPath string `json:"newParentPath,omitempty"`
	UploadIndex   *int   `json:"uploadIndex,omitempty" minimum:"0"`
	// BaselineIndex references an uploaded copy of the content an update_file
	// change was drafted against. The volume workspace revision is already
	// content-aware, so the baseline is accepted for API symmetry with
	// project workspaces but not separately compared.
	BaselineIndex *int   `json:"baselineIndex,omitempty" minimum:"0"`
	BackupID      string `json:"backupId,omitempty"`
	Recursive     bool   `json:"recursive,omitempty"`
}

type VolumeWorkspaceUpdateManifest struct {
	FileTreeRevision string                      `json:"fileTreeRevision" binding:"required"`
	FileChanges      []VolumeWorkspaceFileChange `json:"fileChanges" binding:"required" minItems:"1" maxItems:"500"`
}
