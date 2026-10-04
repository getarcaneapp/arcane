package volume

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	containerdomain "github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	volumebackup "github.com/getarcaneapp/arcane/backend/v2/internal/volume/children/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume/children/workspace"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumes"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
)

type VolumeService struct {
	db               *database.DB
	dockerService    *dockerInternal.DockerClientService
	eventService     *event.EventService
	settingsService  *settings.SettingsService
	imageService     *image.ImageService
	backupVolumeName string
	workspaceLocks   utils.KeyedMutex
	helperMu         sync.Mutex
	helperByVolume   map[string]*volumeHelper
	// helperGroup deduplicates concurrent read-only helper creation per volume.
	// Without it two simultaneous browse requests each create a helper and the
	// second overwrites the first in helperByVolume, orphaning a `sleep infinity`
	// container that pins the volume until restart.
	helperGroup singleflight.Group
	backup      *volumebackup.Service
	workspace   *workspace.Service
}

var ErrVolumeBackupAlreadyRunning = errors.New("a backup is already running for this volume")

const internalVolumePruneFilterValue = libarcane.InternalResourceLabel + "=true"

func NewVolumeService(
	db *database.DB,
	dockerService *dockerInternal.DockerClientService,
	eventService *event.EventService,
	activityService *activity.ActivityService,
	settingsService *settings.SettingsService,
	containerService *containerdomain.ContainerService,
	imageService *image.ImageService,
	engine *backup.Engine,
	s3Destinations *s3.S3DestinationService,
	cfg *config.Config,
	recoveryKeys *backup.RecoveryKeyStore,
) *VolumeService {
	backupVolumeName := ""
	encryptionKey := ""
	workspaceMaxDepth := 50
	workspaceMaxEntries := 10000
	workspaceMaxFileSizeMB := workspacepkg.DefaultMaxFileSizeMB
	if cfg != nil {
		backupVolumeName = cfg.BackupVolumeName
		encryptionKey = cfg.EncryptionKey
		workspaceMaxDepth = cfg.VolumeWorkspaceMaxDepth
		workspaceMaxEntries = cfg.VolumeWorkspaceMaxEntries
		workspaceMaxFileSizeMB = cfg.VolumeWorkspaceMaxFileSizeMB
	}
	if strings.TrimSpace(backupVolumeName) == "" {
		backupVolumeName = "arcane-backups"
	}
	service := &VolumeService{
		db:               db,
		dockerService:    dockerService,
		eventService:     eventService,
		settingsService:  settingsService,
		imageService:     imageService,
		backupVolumeName: backupVolumeName,
		helperByVolume:   make(map[string]*volumeHelper),
	}
	backupDeps := volumebackup.Dependencies{
		Store:            service.backupStoreInternal(),
		DB:               db,
		Docker:           dockerService,
		Events:           eventService,
		Activity:         activityService,
		Settings:         settingsService,
		Engine:           engine,
		S3Destinations:   s3Destinations,
		RecoveryKeys:     recoveryKeys,
		Locks:            &service.workspaceLocks,
		AlreadyRunning:   ErrVolumeBackupAlreadyRunning,
		HelperImage:      service.getVolumeHelperImageInternal,
		AcquireHelper:    service.acquireVolumeHelperInternal,
		Exec:             service.execInContainerInternal,
		BackupVolumeName: backupVolumeName,
		EncryptionKey:    encryptionKey,
	}
	if containerService != nil {
		backupDeps.StopContainer, backupDeps.StartContainer = containerService.StopContainer, containerService.StartContainer
	}
	service.backup = volumebackup.NewService(backupDeps)
	service.workspace = workspace.NewService(workspace.Dependencies{
		Docker:                dockerService,
		Events:                eventService,
		Locks:                 &service.workspaceLocks,
		AcquireHelper:         service.acquireVolumeHelperInternal,
		RequireACFS:           service.requireVolumeHelperACFSInternal,
		Exec:                  service.execInContainerInternal,
		HelperImage:           service.getVolumeHelperImageInternal,
		BackupStorageMount:    service.backup.StorageMount,
		ArchiveFileTarget:     service.backup.ArchiveFileTarget,
		RestoreArchiveMembers: service.backup.RestoreArchiveMembers,
		MaxDepth:              workspaceMaxDepth,
		MaxEntries:            workspaceMaxEntries,
		MaxFileSizeBytes:      workspacepkg.MaxFileSizeBytes(workspaceMaxFileSizeMB),
	})
	return service
}

// SetScheduler injects the dynamic scheduler and admission gate for per-policy
// backup jobs. Agent mode passes them too: agents run their own volume backups.
func (s *VolumeService) SetScheduler(ctx context.Context, dynamicScheduler scheduler.DynamicScheduler, admissionGate *runs.Admission) error {
	return s.backup.SetScheduler(ctx, dynamicScheduler, admissionGate)
}

func (s *VolumeService) RegisterBackupJobsOnStartup(ctx context.Context) {
	s.backup.RegisterJobsOnStartup(ctx)
}

// BackupStorageMount resolves the repository mount shared with system-backup operations.
func (s *VolumeService) BackupStorageMount(ctx context.Context, dockerClient *client.Client, target string, readOnly bool) (mount.Mount, error) {
	return s.backup.StorageMount(ctx, dockerClient, target, readOnly)
}

// HasEnabledBackupPolicy reports whether a volume-level schedule takes precedence over centralized backups.
func (s *VolumeService) HasEnabledBackupPolicy(ctx context.Context, volumeName string) (bool, error) {
	return s.backup.HasEnabledBackupPolicy(ctx, volumeName)
}

// CreateSystemManagedBackup runs the existing volume backup workflow with a transient centralized policy.
func (
	s *VolumeService,
) CreateSystemManagedBackup(
	ctx context.Context,
	volumeName string,
	user usertypes.Actor,
	trigger VolumeBackupTrigger,
	policyID string,
	policy backuptypes.UpdateBackupPolicy,
) (
	*VolumeBackup,
	error,
) {
	entry, err := s.backup.CreateSystemManagedBackup(ctx, volumeName, user, string(trigger), policyID, policy)
	if entry == nil {
		return nil, err
	}
	return volumeBackupFromRecordInternal(entry), err
}

func (s *VolumeService) ReconcileBackup(ctx context.Context, previous scheduler.Run, volumeName string) (scheduler.Outcome, error) {
	return s.backup.ReconcileBackup(ctx, previous, volumeName)
}

// ReconcileInterruptedBackups runs before this process can accept backup work.
func (s *VolumeService) ReconcileInterruptedBackups(ctx context.Context, protectedIDs ...string) error {
	query := s.db.WithContext(ctx).Model(&VolumeBackup{}).Where("status = ?", VolumeBackupStatusRunning)
	if len(protectedIDs) > 0 {
		query = query.Where("id NOT IN ?", protectedIDs)
	}
	return query.Updates(map[string]any{"status": VolumeBackupStatusFailed, "error": "Backup interrupted by Arcane restart"}).Error
}

// MigrateRepositoryPasswords re-keys this instance's own volume backup repositories to the recovery key; other instances re-key their own roots.
func (s *VolumeService) MigrateRepositoryPasswords(ctx context.Context) error {
	return s.backup.MigrateRepositoryPasswords(ctx)
}

func (s *VolumeService) backupStoreInternal() volumebackup.Store {
	return volumebackup.Store{
		Create: func(ctx context.Context, record *volumetypes.Backup) error {
			entry := volumeBackupFromRecordInternal(record)
			if err := s.db.WithContext(ctx).Create(entry).Error; err != nil {
				return err
			}
			record.ID, record.UpdatedAt = entry.ID, entry.UpdatedAt
			return nil
		},
		Save: func(ctx context.Context, record *volumetypes.Backup) error {
			entry := volumeBackupFromRecordInternal(record)
			if err := s.db.WithContext(ctx).Save(entry).Error; err != nil {
				return err
			}
			record.UpdatedAt = entry.UpdatedAt
			return nil
		},
		Fail: func(ctx context.Context, backupID, message string) error {
			return s.db.WithContext(ctx).Model(&VolumeBackup{}).Where("id = ?", backupID).Updates(map[string]any{"status": VolumeBackupStatusFailed, "error": message}).Error
		},
		Delete: func(ctx context.Context, backupID string) error {
			return s.db.WithContext(ctx).Where("id = ?", backupID).Delete(&VolumeBackup{}).Error
		},
		Run: func(ctx context.Context, backupID string) (*volumetypes.Backup, error) {
			var entry VolumeBackup
			if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&entry).Error; err != nil {
				return nil, err
			}
			return entry.recordInternal(), nil
		},
		Runs: func(ctx context.Context, backupIDs []string) ([]*volumetypes.Backup, error) {
			var entries []VolumeBackup
			if err := s.db.WithContext(ctx).Where("id IN ?", backupIDs).Find(&entries).Error; err != nil {
				return nil, err
			}
			records := make([]*volumetypes.Backup, 0, len(entries))
			for i := range entries {
				records = append(records, entries[i].recordInternal())
			}
			return records, nil
		},
		Latest: func(ctx context.Context, policyID string) (*volumetypes.Backup, error) {
			var entry VolumeBackup
			err := s.db.WithContext(ctx).Where("policy_id = ?", policyID).Order("created_at DESC").First(&entry).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			return entry.recordInternal(), nil
		},
		List: s.listBackupsInternal,
		RemoteSnapshotIDs: func(ctx context.Context, destinationID string) ([]string, error) {
			var knownIDs []string
			err := s.db.WithContext(ctx).Model(&VolumeBackup{}).
				Where("s3_destination_id = ? AND remote_snapshot_id <> ''", destinationID).
				Pluck("remote_snapshot_id", &knownIDs).Error
			return knownIDs, err
		},
	}
}

// listBackupsInternal pages one volume's backup history; both or neither management type selected means no type filter.
func (s *VolumeService) listBackupsInternal(ctx context.Context, volumeName string, params pagination.QueryParams) ([]volumetypes.Backup, int64, error) {
	query := s.db.WithContext(ctx).Model(&VolumeBackup{}).Where("volume_name = ?", volumeName)
	var system, managed bool
	for value := range strings.SplitSeq(params.Filters["type"], ",") {
		switch backuptypes.ManagementType(strings.TrimSpace(value)) {
		case backuptypes.ManagementTypeSystem:
			system = true
		case backuptypes.ManagementTypeVolume:
			managed = true
		}
	}
	if system != managed {
		if system {
			query = query.Where("policy_id LIKE ?", backuptypes.SystemVolumePolicyPrefix+"%")
		} else {
			query = query.Where("policy_id NOT LIKE ? OR policy_id IS NULL", backuptypes.SystemVolumePolicyPrefix+"%")
		}
	}

	if params.Search != "" {
		pattern := "%" + params.Search + "%"
		query = query.Where(
			"id LIKE ? OR status LIKE ? OR trigger LIKE ? OR destination LIKE ? OR COALESCE(local_snapshot_id, "+
				"'') LIKE ? OR COALESCE(remote_snapshot_id, '') LIKE ? OR COALESCE(error, '') LIKE ?",
			pattern,
			pattern,
			pattern,
			pattern,
			pattern,
			pattern,
			pattern,
		)
	}

	var totalItems int64
	if err := query.Count(&totalItems).Error; err != nil {
		return nil, 0, err
	}

	sortCol := "created_at"
	sortOrder := "DESC"
	if params.Sort != "" {
		switch params.Sort {
		case "createdAt", "created_at":
			sortCol = "created_at"
		case "id":
			sortCol = "id"
		case "size":
			sortCol = "size"
		case "status":
			sortCol = "status"
		case "trigger":
			sortCol = "trigger"
		case "destination":
			sortCol = "destination"
		case "remoteSnapshotId", "remote_snapshot_id":
			sortCol = "remote_snapshot_id"
		default:
			sortCol = "created_at"
		}

		sortOrder = kit.Ternary(params.Order == pagination.SortDesc, "DESC", "ASC")
	}
	query = query.Order(fmt.Sprintf("%s %s", sortCol, sortOrder))

	if params.Limit > 0 {
		query = query.Offset(params.Start).Limit(params.Limit)
	}

	var entries []VolumeBackup
	if err := query.Find(&entries).Error; err != nil {
		return nil, 0, err
	}
	records := make([]volumetypes.Backup, 0, len(entries))
	for i := range entries {
		record := entries[i].recordInternal()
		record.Type = backuptypes.ManagementTypeForPolicy(record.PolicyID)
		records = append(records, *record)
	}
	return records, totalItems, nil
}

func (s *VolumeService) GetVolumeByName(ctx context.Context, name string) (*volumetypes.Volume, error) {
	slog.DebugContext(ctx, "volume service: get volume", "volume", name)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	volResult, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("volume not found: %w", err)
	}
	vol := volResult.Volume

	localSettings := s.settingsService.GetSettingsConfig()
	usageCtx, usageCancel := context.WithTimeout(ctx, timeouts.GetDuration(localSettings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI))
	defer usageCancel()
	if usageVolumes, ok := docker.GetVolumeUsageDataStaleWhileRevalidate(usageCtx, dockerClient).Get(); ok {
		for _, uv := range usageVolumes {
			if uv.Name == vol.Name && uv.UsageData != nil {
				vol.UsageData = uv.UsageData
				slog.DebugContext(ctx, "attached volume usage data", "volume", vol.Name, "sizeBytes", uv.UsageData.Size, "refCount", uv.UsageData.RefCount)
				break
			}
		}
	}

	v := volumetypes.NewSummary(vol)

	containerIDs, err := docker.GetContainersUsingVolume(ctx, dockerClient, name)
	if err != nil {
		slog.WarnContext(ctx, "failed to get containers using volume", "volume", name, "error", err.Error())
	} else {
		v.Containers = containerIDs
		if len(containerIDs) > 0 {
			v.InUse = true
		}
	}

	return &v, nil
}

func (s *VolumeService) CreateVolume(ctx context.Context, options client.VolumeCreateOptions, user usertypes.Actor) (*volumetypes.Volume, error) {
	slog.DebugContext(ctx, "volume service: create volume", "volume", options.Name, "driver", options.Driver, "user", user.ID)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeVolumeError, "volume", "", options.Name, user.ID, user.Username, "0", err, database.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("volume", options.Name, options.Name)()
	created, err := dockerClient.VolumeCreate(ctx, options)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeVolumeError, "volume", "", options.Name, user.ID, user.Username, "0", err, database.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to create volume: %w", err)
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("volume", created.Volume.Name, created.Volume.Name)()
	vol, err := dockerClient.VolumeInspect(ctx, created.Volume.Name, client.VolumeInspectOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(
			ctx,
			event.EventTypeVolumeError,
			"volume",
			created.Volume.Name,
			created.Volume.Name,
			user.ID,
			user.Username,
			"0",
			err,
			database.JSON{
				"action": "create",
				"driver": options.Driver,
				"step":   "inspect",
			},
		)
		return nil, fmt.Errorf("failed to inspect created volume: %w", err)
	}

	metadata := database.JSON{
		"action": "create",
		"driver": vol.Volume.Driver,
		"name":   vol.Volume.Name,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeCreate, vol.Volume.Name, vol.Volume.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume creation action", "volume", vol.Volume.Name, "error", logErr.Error())
	}

	docker.InvalidateVolumeUsageCache(dockerClient)

	return new(volumetypes.NewSummary(vol.Volume)), nil
}

func (s *VolumeService) DeleteVolume(ctx context.Context, name string, force bool, user usertypes.Actor) error {
	slog.DebugContext(ctx, "volume service: delete volume", "volume", name, "force", force, "user", user.ID)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeVolumeError, "volume", name, name, user.ID, user.Username, "0", err, database.JSON{"action": "delete", "force": force})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Stop any read-only browse helper first; a helper mounting the volume would
	// otherwise block a non-forced VolumeRemove with "volume is in use".
	if stopErr := s.StopHelper(ctx, name); stopErr != nil {
		slog.WarnContext(ctx, "could not stop volume browse helper before delete", "volume", name, "error", stopErr.Error())
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("volume", name, name)()
	if _, volumeRemoveErr := dockerClient.VolumeRemove(ctx, name, client.VolumeRemoveOptions{
		Force: force,
	}); volumeRemoveErr != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeVolumeError, "volume", name, name, user.ID, user.Username, "0", volumeRemoveErr, database.JSON{"action": "delete", "force": force})
		return fmt.Errorf("failed to remove volume: %w", volumeRemoveErr)
	}

	metadata := database.JSON{
		"action": "delete",
		"name":   name,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeDelete, name, name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume deletion action", "volume", name, "error", logErr.Error())
	}

	s.removeHelperEntry(name)
	s.backup.RemovePolicies(ctx, name)
	docker.InvalidateVolumeUsageCache(dockerClient)
	return nil
}

func (s *VolumeService) PruneVolumes(ctx context.Context) (*volumetypes.PruneReport, error) {
	slog.DebugContext(ctx, "volume service: prune volumes")
	return s.PruneVolumesWithOptions(ctx, false)
}

func (s *VolumeService) PruneVolumesWithOptions(ctx context.Context, all bool) (*volumetypes.PruneReport, error) {
	slog.DebugContext(ctx, "volume service: prune volumes with options", "all", all)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Stop all read-only browse helpers first; a helper mounting a volume marks it
	// "in use" and would prevent VolumePrune from reclaiming an otherwise-unused
	// volumetypes. Helpers are re-created on demand on the next browse request.
	s.CleanupHelperContainers(ctx)

	// Docker's VolumesPrune behavior (API v1.42+):
	// - Without 'all' flag: Only removes anonymous (unnamed) volumes that are not in use
	// - With 'all=true' flag: Removes ALL unused volumes (both named and anonymous)
	// Note: Volumes are considered "in use" if referenced by any container (running or stopped)
	volumePruneResult, err := dockerClient.VolumePrune(ctx, buildVolumePruneOptionsInternal(all))
	if err != nil {
		return nil, fmt.Errorf("failed to prune volumes: %w", err)
	}

	metadata := buildVolumePruneMetadataInternal(all, len(volumePruneResult.Report.VolumesDeleted), volumePruneResult.Report.SpaceReclaimed)
	if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeDelete, "", "bulk_prune", usertypes.SystemUser.ID, usertypes.SystemUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume prune action", "error", logErr.Error())
	}

	for _, volumeName := range volumePruneResult.Report.VolumesDeleted {
		s.removeHelperEntry(volumeName)
		s.backup.RemovePolicies(ctx, volumeName)
	}

	docker.InvalidateVolumeUsageCache(dockerClient)

	return &volumetypes.PruneReport{
		VolumesDeleted: volumePruneResult.Report.VolumesDeleted,
		SpaceReclaimed: volumePruneResult.Report.SpaceReclaimed,
	}, nil
}

func buildVolumePruneOptionsInternal(all bool) client.VolumePruneOptions {
	filters := make(client.Filters)
	filters = filters.Add("label!", internalVolumePruneFilterValue)
	return client.VolumePruneOptions{All: all, Filters: filters}
}

func buildVolumePruneMetadataInternal(all bool, volumesDeleted int, spaceReclaimed uint64) database.JSON {
	return database.JSON{
		"action":                    "prune",
		"all":                       all,
		"volumesDeleted":            volumesDeleted,
		"spaceReclaimed":            spaceReclaimed,
		"internalVolumeFilterLabel": internalVolumePruneFilterValue,
	}
}

func (s *VolumeService) GetVolumeUsage(ctx context.Context, name string) (bool, []string, error) {
	slog.DebugContext(ctx, "volume service: get volume usage", "volume", name)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	vol, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err != nil {
		return false, nil, fmt.Errorf("volume not found: %w", err)
	}

	containerIDs, err := docker.GetContainersUsingVolume(ctx, dockerClient, vol.Volume.Name)
	if err != nil {
		return false, nil, fmt.Errorf("failed to get containers using volume: %w", err)
	}

	inUse := len(containerIDs) > 0
	return inUse, containerIDs, nil
}

// VolumeSizeData holds size information for a volume.
type VolumeSizeData struct {
	Size     int64
	RefCount int64
}

// GetVolumeSizes returns disk usage data for all volumes.
// This is a slow operation as it calls Docker's DiskUsage API.
func (s *VolumeService) GetVolumeSizes(ctx context.Context) (map[string]VolumeSizeData, error) {
	slog.DebugContext(ctx, "volume service: get volume sizes")
	localSettings := s.settingsService.GetSettingsConfig()
	apiCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(localSettings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI))
	defer cancel()

	dockerClient, err := s.dockerService.GetClient(apiCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	usageVolumes, err := docker.GetVolumeUsageData(apiCtx, dockerClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get volume usage data: %w", err)
	}

	result := make(map[string]VolumeSizeData, len(usageVolumes))
	for _, v := range usageVolumes {
		if v.UsageData != nil {
			result[v.Name] = VolumeSizeData{
				Size:     v.UsageData.Size,
				RefCount: v.UsageData.RefCount,
			}
		}
	}

	return result, nil
}

func enrichVolumesWithUsageDataInternal(dockerVolumes, usageVolumes []volume.Volume) []volume.Volume {
	usageByName := make(map[string]*volume.UsageData, len(usageVolumes))
	for _, uv := range usageVolumes {
		if uv.Name == "" || uv.UsageData == nil {
			continue
		}
		// Keep first-seen value to preserve previous nested-loop behavior.
		if _, exists := usageByName[uv.Name]; !exists {
			usageByName[uv.Name] = uv.UsageData
		}
	}

	result := make([]volume.Volume, 0, len(dockerVolumes))
	for _, v := range dockerVolumes {
		if usageData, exists := usageByName[v.Name]; exists {
			v.UsageData = usageData
		}

		result = append(result, v)
	}
	return result
}

func (s *VolumeService) buildVolumeContainerMapInternal(ctx context.Context) (map[string][]string, error) {
	containers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	volumeContainerMap := make(map[string][]string)
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name != "" {
				volumeContainerMap[m.Name] = append(volumeContainerMap[m.Name], c.ID)
			}
		}
	}

	return volumeContainerMap, nil
}

func (s *VolumeService) buildVolumePaginationConfigInternal() pagination.Config[volumetypes.Volume] {
	return pagination.Config[volumetypes.Volume]{
		SearchAccessors: []pagination.SearchAccessor[volumetypes.Volume]{
			func(v volumetypes.Volume) (string, error) { return v.Name, nil },
			func(v volumetypes.Volume) (string, error) { return v.Driver, nil },
			func(v volumetypes.Volume) (string, error) { return v.Mountpoint, nil },
			func(v volumetypes.Volume) (string, error) { return v.Scope, nil },
		},
		SortBindings:    s.buildVolumeSortBindingsInternal(),
		FilterAccessors: buildVolumeFilterAccessorsInternal(),
	}
}

func (s *VolumeService) buildVolumeSortBindingsInternal() []pagination.SortBinding[volumetypes.Volume] {
	createdSortFn := s.compareVolumeCreatedInternal

	return []pagination.SortBinding[volumetypes.Volume]{
		{
			Key: "name",
			Fn:  func(a, b volumetypes.Volume) int { return strings.Compare(a.Name, b.Name) },
		},
		{
			Key: "driver",
			Fn:  func(a, b volumetypes.Volume) int { return strings.Compare(a.Driver, b.Driver) },
		},
		{
			Key: "mountpoint",
			Fn:  func(a, b volumetypes.Volume) int { return strings.Compare(a.Mountpoint, b.Mountpoint) },
		},
		{
			Key: "scope",
			Fn:  func(a, b volumetypes.Volume) int { return strings.Compare(a.Scope, b.Scope) },
		},
		{
			Key: "created",
			Fn:  createdSortFn,
		},
		{
			Key: "createdAt",
			Fn:  createdSortFn,
		},
		{
			Key: "inUse",
			Fn: func(a, b volumetypes.Volume) int {
				if a.InUse == b.InUse {
					return 0
				}
				return kit.Ternary(a.InUse, -1, 1)
			},
		},
		{
			Key: "size",
			Fn:  compareVolumeSizesInternal,
		},
	}
}

func compareVolumeSizesInternal(a, b volumetypes.Volume) int {
	aSize := a.Size
	bSize := b.Size

	if aSize == 0 && a.UsageData != nil {
		aSize = a.UsageData.Size
	}
	if bSize == 0 && b.UsageData != nil {
		bSize = b.UsageData.Size
	}

	if aSize == bSize {
		return strings.Compare(a.Name, b.Name)
	}
	return kit.Ternary(aSize < bSize, -1, 1)
}

func (s *VolumeService) compareVolumeCreatedInternal(a, b volumetypes.Volume) int {
	aTime, aOk := parseVolumeCreatedAtInternal(a.CreatedAt).Get()
	bTime, bOk := parseVolumeCreatedAtInternal(b.CreatedAt).Get()
	if aOk && bOk {
		if aTime.Before(bTime) {
			return -1
		}
		return kit.Ternary(aTime.After(bTime), 1, 0)
	}
	return strings.Compare(a.CreatedAt, b.CreatedAt)
}

func parseVolumeCreatedAtInternal(createdAt string) mo.Option[time.Time] {
	if createdAt == "" {
		return mo.None[time.Time]()
	}
	if parsed, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		return mo.Some(parsed)
	}
	if parsed, err := time.Parse(time.RFC3339, createdAt); err == nil {
		return mo.Some(parsed)
	}
	return mo.None[time.Time]()
}

func buildVolumeFilterAccessorsInternal() []pagination.FilterAccessor[volumetypes.Volume] {
	return []pagination.FilterAccessor[volumetypes.Volume]{
		{
			Key: "inUse",
			Fn: func(v volumetypes.Volume, filterValue string) bool {
				return kit.Ternary(filterValue == "true", v.InUse, filterValue != "false" || !v.InUse)
			},
		},
	}
}

func calculateVolumeUsageCountsInternal(items []volumetypes.Volume) volumetypes.UsageCounts {
	counts := volumetypes.UsageCounts{
		Total: len(items),
	}
	for _, v := range items {
		if v.InUse {
			counts.Inuse++
		} else {
			counts.Unused++
		}
	}
	return counts
}

// CountUsageFromSnapshot derives volume usage counts from an
// already-fetched volume and container listing. It mirrors the semantics of
// ListVolumesPaginated — internal volumes are excluded and a volume counts as
// in use when any container mounts it — so dashboard tile numbers agree with
// the volumes page.
func (s *VolumeService) CountUsageFromSnapshot(dockerVolumes []volume.Volume, containers []container.Summary) volumetypes.UsageCounts {
	inUse := make(map[string]struct{})
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name != "" {
				inUse[m.Name] = struct{}{}
			}
		}
	}

	counts := volumetypes.UsageCounts{}
	for _, v := range dockerVolumes {
		if s.isInternalVolumeInternal(volumetypes.NewSummary(v)) {
			continue
		}
		counts.Total++
		if _, ok := inUse[v.Name]; ok {
			counts.Inuse++
		} else {
			counts.Unused++
		}
	}
	return counts
}

func (s *VolumeService) isInternalVolumeInternal(v volumetypes.Volume) bool {
	if strings.EqualFold(strings.TrimSpace(v.Name), strings.TrimSpace(s.backupVolumeName)) {
		return true
	}

	internal, _ := kit.ParseBool(v.Labels[libarcane.InternalResourceLabel])
	return internal
}

var generatedAnonymousVolumeNameInternal = regexp.MustCompile(`^[a-f0-9]{64}$`)

func isAnonymousVolumeInternal(v volume.Volume) bool {
	if _, ok := v.Labels["com.docker.volume.anonymous"]; ok {
		return true
	}
	return generatedAnonymousVolumeNameInternal.MatchString(v.Name)
}

func mountedVolumeNamesInternal(mounts []container.MountPoint) map[string]struct{} {
	names := make(map[string]struct{})
	for _, item := range mounts {
		if item.Type != mount.TypeVolume {
			continue
		}
		name := cmp.Or(strings.TrimSpace(item.Name), strings.TrimSpace(item.Source))
		if name != "" {
			names[name] = struct{}{}
		}
	}
	return names
}

// ListBackupVolumeOptions returns the current non-internal volumes used by centralized backup selection.
func (s *VolumeService) ListBackupVolumeOptions(ctx context.Context) ([]backuptypes.SystemVolumeBackupOption, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}
	result, err := dockerClient.VolumeList(ctx, client.VolumeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker volumes: %w", err)
	}
	arcaneVolumes := make(map[string]struct{})
	if inspect, inspectErr := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient); inspectErr == nil && inspect != nil {
		arcaneVolumes = mountedVolumeNamesInternal(inspect.Mounts)
	} else if inspectErr != nil {
		slog.DebugContext(ctx, "volume service: could not identify Arcane-mounted volumes for centralized backups", "error", inspectErr)
	}
	options := make([]backuptypes.SystemVolumeBackupOption, 0, len(result.Items))
	for _, item := range result.Items {
		if s.isInternalVolumeInternal(volumetypes.NewSummary(item)) {
			continue
		}
		if _, isArcaneVolume := arcaneVolumes[item.Name]; isArcaneVolume {
			continue
		}
		options = append(options, backuptypes.SystemVolumeBackupOption{
			Name: item.Name, Anonymous: isAnonymousVolumeInternal(item), Available: true,
		})
	}
	return options, nil
}

func (s *VolumeService) ListVolumesPaginated(ctx context.Context, params pagination.QueryParams, includeInternal bool) ([]volumetypes.Volume, pagination.Response, volumetypes.UsageCounts, error) {
	startedAt := time.Now()
	slog.DebugContext(
		ctx,
		"volume service: list volumes paginated",
		"search",
		params.Search,
		"sort",
		params.Sort,
		"order",
		params.Order,
		"start",
		params.Start,
		"limit",
		params.Limit,
		"includeInternal",
		includeInternal,
	)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, pagination.Response{}, volumetypes.UsageCounts{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Run volume list and container list in parallel for better performance
	type volumeListResult struct {
		volumes []volume.Volume
		err     error
	}
	type containerMapResult struct {
		containerMap map[string][]string
		err          error
	}

	volChan := make(chan volumeListResult, 1)
	containerChan := make(chan containerMapResult, 1)

	localSettings := s.settingsService.GetSettingsConfig()
	apiCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(localSettings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI))
	defer cancel()

	go func(ctx context.Context) {
		volListBody, volumeListErr := dockerClient.VolumeList(ctx, client.VolumeListOptions{})
		volChan <- volumeListResult{volumes: volListBody.Items, err: volumeListErr}
	}(apiCtx)

	go func(ctx context.Context) {
		containerMap, buildVolumeContainerMapErr := s.buildVolumeContainerMapInternal(ctx)
		containerChan <- containerMapResult{containerMap: containerMap, err: buildVolumeContainerMapErr}
	}(apiCtx)

	// Wait for both results
	volResult := <-volChan
	if volResult.err != nil {
		return nil, pagination.Response{}, volumetypes.UsageCounts{}, fmt.Errorf("failed to list Docker volumes: %w", volResult.err)
	}

	containerResult := <-containerChan
	volumeContainerMap := containerResult.containerMap
	if containerResult.err != nil {
		slog.WarnContext(ctx, "failed to build volume-container map", "error", containerResult.err.Error())
		volumeContainerMap = make(map[string][]string)
	}

	effectiveParams := params
	usageCacheSnapshot := "not_requested"

	// Size sorting consumes the current cache snapshot and refreshes it in the
	// background so this list request never waits for Docker's DiskUsage call.
	var usageVolumes []volume.Volume
	if params.Sort == "size" {
		if uv, found := docker.GetVolumeUsageDataStaleWhileRevalidate(apiCtx, dockerClient).Get(); found && (len(uv) > 0 || len(volResult.volumes) == 0) {
			usageVolumes = uv
			usageCacheSnapshot = "available"
		} else {
			usageCacheSnapshot = "missing"
			effectiveParams.Sort = "name"
			effectiveParams.Order = pagination.SortAsc
		}
	}

	enrichedVolumes := enrichVolumesWithUsageDataInternal(volResult.volumes, usageVolumes)

	items := make([]volumetypes.Volume, 0, len(enrichedVolumes))
	for _, v := range enrichedVolumes {
		volDto := volumetypes.NewSummary(v)
		if !includeInternal && s.isInternalVolumeInternal(volDto) {
			continue
		}
		if containerIDs, ok := volumeContainerMap[v.Name]; ok {
			volDto.Containers = containerIDs
			if len(containerIDs) > 0 {
				volDto.InUse = true
			}
		}
		items = append(items, volDto)
	}

	paginationConfig := s.buildVolumePaginationConfigInternal()
	result := paginationConfig.SearchOrderAndPaginate(items, effectiveParams)
	counts := calculateVolumeUsageCountsInternal(items)
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, effectiveParams)
	slog.DebugContext(
		ctx, "volume service: listed volumes",
		"dockerHost", dockerClient.DaemonHost(),
		"requestedSort", params.Sort,
		"requestedOrder", params.Order,
		"effectiveSort", effectiveParams.Sort,
		"effectiveOrder", effectiveParams.Order,
		"usageCacheSnapshot", usageCacheSnapshot,
		"dockerVolumes", len(volResult.volumes),
		"usageVolumes", len(usageVolumes),
		"includedVolumes", len(items),
		"matchedVolumes", result.TotalCount,
		"returnedVolumes", len(result.Items),
		"containerVolumeCount", len(volumeContainerMap),
		"filterCount", len(params.Filters),
		"currentPage", paginationResp.CurrentPage,
		"totalPages", paginationResp.TotalPages,
		"duration", time.Since(startedAt),
	)

	return result.Items, paginationResp, counts, nil
}

// RenameVolume copies an unused volume to a new name and removes the source.
func (s *VolumeService) RenameVolume(ctx context.Context, oldName, newName string, user usertypes.Actor) (*volumetypes.Volume, error) {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" || oldName == newName {
		return nil, common.ErrVolumeRenameInvalid
	}
	if strings.EqualFold(oldName, s.backupVolumeName) || strings.EqualFold(newName, s.backupVolumeName) {
		return nil, common.ErrVolumeRenameProtected
	}

	defer s.workspaceLocks.Lock(oldName)()

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "connect", err)
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	source, err := dockerClient.VolumeInspect(ctx, oldName, client.VolumeInspectOptions{})
	if err != nil {
		if errdefs.IsNotFound(err) {
			err = common.Classify(common.ErrNotFound, err)
		}
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "inspect", err)
		return nil, fmt.Errorf("inspect source volume: %w", err)
	}
	if s.isInternalVolumeInternal(volumetypes.NewSummary(source.Volume)) {
		return nil, common.ErrVolumeRenameProtected
	}

	// Stop any read-only browse helper first; a helper mounting the volume would
	// otherwise fail the detached-source check with "in use".
	if stopErr := s.StopHelper(ctx, oldName); stopErr != nil {
		slog.WarnContext(ctx, "could not stop volume browse helper before rename", "volume", oldName, "error", stopErr.Error())
	}

	migration, err := volumes.PlanRename(ctx, dockerClient, oldName, newName, s.toolsImageInternal())
	if err != nil {
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "plan", err)
		return nil, err
	}
	if applyErr := migration.Apply(ctx); applyErr != nil {
		if errdefs.IsInvalidArgument(applyErr) {
			applyErr = common.Classify(common.ErrBadRequest, applyErr)
		}
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "apply", applyErr)
		return nil, applyErr
	}

	if renameVolumeMetadataErr := s.renameVolumeMetadataInternal(ctx, oldName, newName); renameVolumeMetadataErr != nil {
		rollbackErr := migration.Rollback(ctx)
		combinedErr := errors.Join(fmt.Errorf("rename volume metadata: %w", renameVolumeMetadataErr), rollbackErr)
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "metadata", combinedErr)
		return nil, combinedErr
	}

	if committer, ok := migration.(volumetypes.Committer); ok {
		if commitErr := committer.Commit(ctx); commitErr != nil {
			if _, localOk := errors.AsType[*volumetypes.SourceCleanupError](commitErr); localOk {
				// The copy and metadata are committed; only removing the source
				// failed, so the rename itself succeeded.
				slog.WarnContext(ctx, "volume renamed but source volume could not be removed", "oldVolume", oldName, "newVolume", newName, "error", commitErr.Error())
			} else {
				metadataErr := s.renameVolumeMetadataInternal(ctx, newName, oldName)
				rollbackErr := migration.Rollback(ctx)
				combinedErr := errors.Join(commitErr, metadataErr, rollbackErr)
				s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "commit", combinedErr)
				return nil, combinedErr
			}
		}
	}

	s.removeHelperEntry(oldName)
	docker.InvalidateVolumeUsageCache(dockerClient)

	renamed, err := s.GetVolumeByName(ctx, newName)
	if err != nil {
		s.logVolumeRenameErrorInternal(ctx, oldName, newName, user, "inspect-renamed", err)
		return nil, fmt.Errorf("inspect renamed volume: %w", err)
	}

	metadata := database.JSON{"action": "rename", "oldName": oldName, "newName": newName}
	if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeRename, newName, newName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume rename action", "oldVolume", oldName, "newVolume", newName, "error", logErr)
	}

	return renamed, nil
}

// Backup jobs are keyed by policy ID and re-read the policy row on every run,
// so renaming volume_name requires no job rescheduling.
func (s *VolumeService) renameVolumeMetadataInternal(ctx context.Context, oldName, newName string) error {
	if s.db == nil {
		return nil
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&VolumeBackup{}).Where("volume_name = ?", oldName).Update("volume_name", newName).Error; err != nil {
			return fmt.Errorf("rename volume backup history: %w", err)
		}
		if err := s.backup.RenamePolicies(tx, oldName, newName); err != nil {
			return fmt.Errorf("rename volume backup policies: %w", err)
		}
		return nil
	})
}

func (s *VolumeService) logVolumeRenameErrorInternal(ctx context.Context, oldName, newName string, user usertypes.Actor, step string, err error) {
	s.eventService.LogErrorEvent(ctx, event.EventTypeVolumeError, "volume", oldName, oldName, user.ID, user.Username, "0", err, database.JSON{
		"action":  "rename",
		"step":    step,
		"oldName": oldName,
		"newName": newName,
	})
}
