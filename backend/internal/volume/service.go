package volume

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.getarcane.app/docker"
	kit "go.getarcane.app/kit/pkg"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	containerdomain "github.com/getarcaneapp/arcane/backend/v3/internal/container"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/image"
	"github.com/getarcaneapp/arcane/backend/v3/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	volumebackup "github.com/getarcaneapp/arcane/backend/v3/internal/volume/children/backup"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume/children/workspace"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/volumes"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
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

const (
	internalVolumePruneFilterValue = libarcane.InternalResourceLabel + "=true"
	legacyDefaultEncryptionKey     = "arcane-dev-key-32-characters!!!"
)

func NewVolumeService(
	db *database.DB,
	dockerService *dockerInternal.DockerClientService,
	eventService *event.EventService,
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
	if cfg != nil {
		backupVolumeName = cfg.BackupVolumeName
		encryptionKey = cmp.Or(cfg.EncryptionKey, legacyDefaultEncryptionKey)
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
		Store:            service.backupStore(),
		DB:               db,
		Docker:           dockerService,
		Events:           eventService,
		Settings:         settingsService,
		Engine:           engine,
		S3Destinations:   s3Destinations,
		RecoveryKeys:     recoveryKeys,
		Locks:            &service.workspaceLocks,
		AlreadyRunning:   ErrVolumeBackupAlreadyRunning,
		HelperImage:      service.getVolumeHelperImage,
		AcquireHelper:    service.acquireVolumeHelper,
		Exec:             service.execInContainer,
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
		AcquireHelper:         service.acquireVolumeHelper,
		RequireACFS:           service.requireVolumeHelperACFS,
		Exec:                  service.execInContainer,
		HelperImage:           service.getVolumeHelperImage,
		BackupStorageMount:    service.backup.StorageMount,
		ArchiveFileTarget:     service.backup.ArchiveFileTarget,
		RestoreArchiveMembers: service.backup.RestoreArchiveMembers,
		Settings:              settingsService,
	})
	return service
}

// SetScheduler injects the dynamic scheduler and admission gate for per-policy
// backup jobs. Agent mode passes them too: agents run their own volume backups.
func (s *VolumeService) SetScheduler(ctx context.Context, dynamicScheduler types.DynamicScheduler, admissionGate *runs.Admission) error {
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
func (s *VolumeService) CreateSystemManagedBackup(
	ctx context.Context,
	volumeName string,
	user types.Actor,
	trigger VolumeBackupTrigger,
	policyID string,
	policy types.UpdateBackupPolicy,
) (*VolumeBackup, error) {
	entry, err := s.backup.CreateSystemManagedBackup(ctx, volumeName, user, string(trigger), policyID, policy)
	if entry == nil {
		return nil, err
	}
	return volumeBackupFromRecord(entry), err
}

func (s *VolumeService) ReconcileBackup(ctx context.Context, previous types.SchedulerRun, volumeName string) (types.SchedulerOutcome, error) {
	return s.backup.ReconcileBackup(ctx, previous, volumeName)
}

// ReconcileInterruptedBackups runs before this process can accept backup work.
// RegisterWorkflows defines the backup workflows while the host is still unstarted.
func (s *VolumeService) RegisterWorkflows(engine *flow.Engine) error {
	return s.backup.RegisterWorkflows(engine)
}

// ReconcileInterruptedBackups fails backups left running by the previous process and watches the manual
// backups that saved workflows resume.
func (s *VolumeService) ReconcileInterruptedBackups(ctx context.Context, protectedIDs ...string) error {
	query := s.db.WithContext(ctx).Model(&VolumeBackup{}).Where("status = ?", VolumeBackupStatusRunning)
	if len(protectedIDs) > 0 {
		query = query.Where("id NOT IN ?", protectedIDs)
	}
	if err := query.Updates(map[string]any{"status": VolumeBackupStatusFailed, "error": "Backup interrupted by Arcane restart"}).Error; err != nil {
		return err
	}
	return s.backup.WatchAcceptedBackups(ctx)
}

// PruneLocalBackupRepository frees the space of deleted local volume backups.
func (s *VolumeService) PruneLocalBackupRepository(ctx context.Context) error {
	return s.backup.PruneLocalRepository(ctx)
}

// MigrateRepositoryPasswords re-keys this instance's own volume backup repositories to the recovery key; other instances re-key their own roots.
func (s *VolumeService) MigrateRepositoryPasswords(ctx context.Context, previousKey, newKey string) error {
	return s.backup.MigrateRepositoryPasswords(ctx, previousKey, newKey)
}

func (s *VolumeService) backupStore() volumebackup.Store {
	return volumebackup.Store{
		Create: func(ctx context.Context, record *types.VolumeBackup) error {
			entry := volumeBackupFromRecord(record)
			if err := s.db.WithContext(ctx).Create(entry).Error; err != nil {
				return err
			}
			record.ID, record.UpdatedAt = entry.ID, entry.UpdatedAt
			return nil
		},
		Save: func(ctx context.Context, record *types.VolumeBackup) error {
			entry := volumeBackupFromRecord(record)
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
		Run: func(ctx context.Context, backupID string) (*types.VolumeBackup, error) {
			var entry VolumeBackup
			if err := s.db.WithContext(ctx).Where("id = ?", backupID).First(&entry).Error; err != nil {
				return nil, err
			}
			return entry.record(), nil
		},
		Runs: func(ctx context.Context, backupIDs []string) ([]*types.VolumeBackup, error) {
			var entries []VolumeBackup
			if err := s.db.WithContext(ctx).Where("id IN ?", backupIDs).Find(&entries).Error; err != nil {
				return nil, err
			}
			records := make([]*types.VolumeBackup, 0, len(entries))
			for i := range entries {
				records = append(records, entries[i].record())
			}
			return records, nil
		},
		Latest: func(ctx context.Context, policyID string) (*types.VolumeBackup, error) {
			var entry VolumeBackup
			err := s.db.WithContext(ctx).Where("policy_id = ?", policyID).Order("created_at DESC").First(&entry).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			return entry.record(), nil
		},
		List: s.listBackups,
		RemoteSnapshotIDs: func(ctx context.Context, destinationID string) ([]string, error) {
			var knownIDs []string
			err := s.db.WithContext(ctx).Model(&VolumeBackup{}).
				Where("s3_destination_id = ? AND remote_snapshot_id <> ''", destinationID).
				Pluck("remote_snapshot_id", &knownIDs).Error
			return knownIDs, err
		},
	}
}

// listBackups pages one volume's backup history; both or neither management type selected means no type filter.
func (s *VolumeService) listBackups(ctx context.Context, volumeName string, params pagination.QueryParams) ([]types.VolumeBackup, pagination.Response, error) {
	query := s.db.WithContext(ctx).Model(&VolumeBackup{}).Where("volume_name = ?", volumeName)
	var system, managed bool
	for value := range strings.SplitSeq(params.Filters["type"], ",") {
		switch types.BackupManagementType(strings.TrimSpace(value)) {
		case types.BackupManagementTypeSystem:
			system = true
		case types.BackupManagementTypeVolume:
			managed = true
		}
	}
	if system != managed {
		if system {
			query = query.Where("policy_id LIKE ?", types.SystemVolumePolicyPrefix+"%")
		} else {
			query = query.Where("policy_id NOT LIKE ? OR policy_id IS NULL", types.SystemVolumePolicyPrefix+"%")
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

	var entries []VolumeBackup
	page, err := pagination.PaginateAndSortDB(pagination.QueryParams{Params: params.Params}, query, &entries)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	records := make([]types.VolumeBackup, 0, len(entries))
	for i := range entries {
		record := entries[i].record()
		record.Type = types.BackupManagementTypeForPolicy(record.PolicyID)
		records = append(records, *record)
	}
	return records, page, nil
}

func (s *VolumeService) GetVolumeByName(ctx context.Context, name string) (*types.Volume, error) {
	slog.DebugContext(ctx, "volume service: get volume", "volume", name)
	volResult, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeInspectResult, error) {
		return dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("volume not found: %w", err)
	}
	usage, _ := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (mo.Option[[]volume.Volume], error) {
		return docker.GetVolumeUsageDataStaleWhileRevalidate(ctx, dockerClient), nil
	})
	v := types.NewVolume(enrichVolumesWithUsageData([]volume.Volume{volResult.Volume}, usage.OrEmpty())[0])

	containerIDs, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) ([]string, error) {
		return docker.GetContainersUsingVolume(ctx, dockerClient, name)
	})
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

func (s *VolumeService) CreateVolume(ctx context.Context, options client.VolumeCreateOptions, user types.Actor) (*types.Volume, error) {
	slog.DebugContext(ctx, "volume service: create volume", "volume", options.Name, "driver", options.Driver, "user", user.ID)
	if strings.TrimSpace(options.Name) == "" {
		return nil, common.Classify(common.ErrBadRequest, errors.New("volume name is required"))
	}
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeVolumeError, "volume", "", options.Name, user.ID, user.Username, "0", err, database.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("volume", options.Name, options.Name)()
	created, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeCreateResult, error) {
		return dockerClient.VolumeCreate(ctx, options)
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeVolumeError, "volume", "", options.Name, user.ID, user.Username, "0", err, database.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to create volume: %w", err)
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("volume", created.Volume.Name, created.Volume.Name)()
	vol, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeInspectResult, error) {
		return dockerClient.VolumeInspect(ctx, created.Volume.Name, client.VolumeInspectOptions{})
	})
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

	return new(types.NewVolume(vol.Volume)), nil
}

func (s *VolumeService) DeleteVolume(ctx context.Context, name string, force bool, user types.Actor) error {
	slog.DebugContext(ctx, "volume service: delete volume", "volume", name, "force", force, "user", user.ID)
	// Clients may not know this environment's backup volume name, so its removal is refused here.
	if s.IsBackupVolume(name) {
		return common.ErrVolumeDeleteProtected
	}
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
	if _, volumeRemoveErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeRemoveResult, error) {
		return dockerClient.VolumeRemove(ctx, name, client.VolumeRemoveOptions{Force: force})
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

	s.takeHelperID(name)
	s.backup.RemovePolicies(ctx, name)
	docker.InvalidateVolumeUsageCache(dockerClient)
	return nil
}

func (s *VolumeService) PruneVolumes(ctx context.Context) (*types.VolumePruneReport, error) {
	slog.DebugContext(ctx, "volume service: prune volumes")
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Stop idle read-only browse helpers first; a helper mounting a volume marks it
	// "in use" and would prevent VolumePrune from reclaiming an otherwise-unused
	// volume. Helpers are re-created on demand on the next browse request.
	if _, reapErr := s.ReapIdleHelpers(ctx, 0); reapErr != nil {
		slog.WarnContext(ctx, "could not remove idle volume helpers before prune", "error", reapErr.Error())
	}

	// Docker's VolumesPrune behavior (API v1.42+):
	// - Without 'all' flag: Only removes anonymous (unnamed) volumes that are not in use
	// - With 'all=true' flag: Removes ALL unused volumes (both named and anonymous)
	// Note: Volumes are considered "in use" if referenced by any container (running or stopped)
	volumePruneResult, err := dockerClient.VolumePrune(ctx, client.VolumePruneOptions{
		Filters: make(client.Filters).Add("label!", internalVolumePruneFilterValue),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to prune volumes: %w", err)
	}

	metadata := database.JSON{
		"action":                    "prune",
		"all":                       false,
		"volumesDeleted":            len(volumePruneResult.Report.VolumesDeleted),
		"spaceReclaimed":            volumePruneResult.Report.SpaceReclaimed,
		"internalVolumeFilterLabel": internalVolumePruneFilterValue,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, event.EventTypeVolumeDelete, "", "bulk_prune", types.SystemUser.ID, types.SystemUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume prune action", "error", logErr.Error())
	}

	for _, volumeName := range volumePruneResult.Report.VolumesDeleted {
		s.takeHelperID(volumeName)
		s.backup.RemovePolicies(ctx, volumeName)
	}

	docker.InvalidateVolumeUsageCache(dockerClient)

	return &types.VolumePruneReport{
		VolumesDeleted: volumePruneResult.Report.VolumesDeleted,
		SpaceReclaimed: volumePruneResult.Report.SpaceReclaimed,
	}, nil
}

func (s *VolumeService) GetVolumeUsage(ctx context.Context, name string) (bool, []string, error) {
	slog.DebugContext(ctx, "volume service: get volume usage", "volume", name)
	vol, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeInspectResult, error) {
		return dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	})
	if err != nil {
		return false, nil, fmt.Errorf("volume not found: %w", err)
	}

	containerIDs, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) ([]string, error) {
		return docker.GetContainersUsingVolume(ctx, dockerClient, vol.Volume.Name)
	})
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
	usageVolumes, err := s.dockerService.Call(ctx, docker.GetVolumeUsageData)
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

func enrichVolumesWithUsageData(dockerVolumes, usageVolumes []volume.Volume) []volume.Volume {
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

func (s *VolumeService) buildVolumePaginationConfig() pagination.Config[types.Volume] {
	createdSortFn := func(a, b types.Volume) int {
		parseCreatedAt := func(createdAt string) (time.Time, bool) {
			for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
				if parsed, err := time.Parse(layout, createdAt); err == nil {
					return parsed, true
				}
			}
			return time.Time{}, false
		}
		aTime, aOk := parseCreatedAt(a.CreatedAt)
		bTime, bOk := parseCreatedAt(b.CreatedAt)
		if aOk && bOk {
			return aTime.Compare(bTime)
		}
		return strings.Compare(a.CreatedAt, b.CreatedAt)
	}

	return pagination.Config[types.Volume]{
		SearchAccessors: []pagination.SearchAccessor[types.Volume]{
			func(v types.Volume) (string, error) { return v.Name, nil },
			func(v types.Volume) (string, error) { return v.Driver, nil },
			func(v types.Volume) (string, error) { return v.Mountpoint, nil },
			func(v types.Volume) (string, error) { return v.Scope, nil },
		},
		SortBindings: []pagination.SortBinding[types.Volume]{
			{
				Key: "name",
				Fn:  func(a, b types.Volume) int { return strings.Compare(a.Name, b.Name) },
			},
			{
				Key: "driver",
				Fn:  func(a, b types.Volume) int { return strings.Compare(a.Driver, b.Driver) },
			},
			{
				Key: "mountpoint",
				Fn:  func(a, b types.Volume) int { return strings.Compare(a.Mountpoint, b.Mountpoint) },
			},
			{
				Key: "scope",
				Fn:  func(a, b types.Volume) int { return strings.Compare(a.Scope, b.Scope) },
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
				Fn: func(a, b types.Volume) int {
					if a.InUse == b.InUse {
						return 0
					}
					return kit.Ternary(a.InUse, -1, 1)
				},
			},
			{
				Key: "size",
				Fn: func(a, b types.Volume) int {
					aSize := cmp.Or(a.Size, kit.FromPtr(a.UsageData).Size)
					bSize := cmp.Or(b.Size, kit.FromPtr(b.UsageData).Size)
					return cmp.Or(cmp.Compare(aSize, bSize), strings.Compare(a.Name, b.Name))
				},
			},
		},
		FilterAccessors: []pagination.FilterAccessor[types.Volume]{
			{
				Key: "inUse",
				Fn: func(v types.Volume, filterValue string) bool {
					return kit.Ternary(filterValue == "true", v.InUse, filterValue != "false" || !v.InUse)
				},
			},
		},
	}
}

// CountUsageFromSnapshot derives usage counts from fetched volumes and containers with ListVolumesPaginated's
// semantics (internal volumes excluded, mounted means in use) so dashboard tiles match the volumes page.
func (s *VolumeService) CountUsageFromSnapshot(dockerVolumes []volume.Volume, containers []container.Summary) types.VolumeUsageCounts {
	inUse := make(map[string]struct{})
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name != "" {
				inUse[m.Name] = struct{}{}
			}
		}
	}

	counts := types.VolumeUsageCounts{}
	for _, v := range dockerVolumes {
		if s.isInternalVolume(types.NewVolume(v)) {
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

// IsBackupVolume reports whether name is the volume that stores this environment's backups.
func (s *VolumeService) IsBackupVolume(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(s.backupVolumeName))
}

func (s *VolumeService) isInternalVolume(v types.Volume) bool {
	if s.IsBackupVolume(v.Name) {
		return true
	}

	internal, _ := kit.ParseBool(v.Labels[libarcane.InternalResourceLabel])
	return internal
}

var generatedAnonymousVolumeName = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ListBackupVolumeOptions returns the current non-internal volumes used by centralized backup selection.
func (s *VolumeService) ListBackupVolumeOptions(ctx context.Context) ([]types.SystemVolumeBackupOption, error) {
	result, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeListResult, error) {
		return dockerClient.VolumeList(ctx, client.VolumeListOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker volumes: %w", err)
	}
	inspect, inspectErr := s.dockerService.Call(ctx, libarcane.InspectCurrentArcaneContainer)
	if inspectErr != nil {
		slog.DebugContext(ctx, "volume service: could not identify Arcane-mounted volumes for centralized backups", "error", inspectErr)
	}
	arcaneMounts := kit.FromPtr(inspect).Mounts
	options := make([]types.SystemVolumeBackupOption, 0, len(result.Items))
	for _, item := range result.Items {
		if s.isInternalVolume(types.NewVolume(item)) || slices.ContainsFunc(arcaneMounts, func(m container.MountPoint) bool {
			return m.Type == mount.TypeVolume && cmp.Or(strings.TrimSpace(m.Name), strings.TrimSpace(m.Source)) == item.Name
		}) {
			continue
		}
		_, anonymousLabel := item.Labels["com.docker.volume.anonymous"]
		options = append(options, types.SystemVolumeBackupOption{
			Name:      item.Name,
			Anonymous: anonymousLabel || generatedAnonymousVolumeName.MatchString(item.Name),
			Available: true,
		})
	}
	return options, nil
}

func (s *VolumeService) ListVolumesPaginated(ctx context.Context, params pagination.QueryParams, includeInternal bool) ([]types.Volume, pagination.Response, types.VolumeUsageCounts, error) {
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

	go func() {
		volListBody, volumeListErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeListResult, error) {
			return dockerClient.VolumeList(ctx, client.VolumeListOptions{})
		})
		volChan <- volumeListResult{volumes: volListBody.Items, err: volumeListErr}
	}()

	go func() {
		containers, listErr := s.dockerService.ListContainers(ctx)
		if listErr != nil {
			containerChan <- containerMapResult{err: fmt.Errorf("failed to list containers: %w", listErr)}
			return
		}
		containerMap := make(map[string][]string)
		for _, c := range containers {
			for _, m := range c.Mounts {
				if m.Type == mount.TypeVolume && m.Name != "" {
					containerMap[m.Name] = append(containerMap[m.Name], c.ID)
				}
			}
		}
		containerChan <- containerMapResult{containerMap: containerMap}
	}()

	// Wait for both results
	volResult := <-volChan
	if volResult.err != nil {
		return nil, pagination.Response{}, types.VolumeUsageCounts{}, fmt.Errorf("failed to list Docker volumes: %w", volResult.err)
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
		usage, _ := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (mo.Option[[]volume.Volume], error) {
			return docker.GetVolumeUsageDataStaleWhileRevalidate(ctx, dockerClient), nil
		})
		if uv, found := usage.Get(); found && (len(uv) > 0 || len(volResult.volumes) == 0) {
			usageVolumes = uv
			usageCacheSnapshot = "available"
		} else {
			usageCacheSnapshot = "missing"
			effectiveParams.Sort = "name"
			effectiveParams.Order = pagination.SortAsc
		}
	}

	enrichedVolumes := enrichVolumesWithUsageData(volResult.volumes, usageVolumes)

	items := make([]types.Volume, 0, len(enrichedVolumes))
	for _, v := range enrichedVolumes {
		volDto := types.NewVolume(v)
		if !includeInternal && s.isInternalVolume(volDto) {
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

	result := s.buildVolumePaginationConfig().SearchOrderAndPaginate(items, effectiveParams)
	inUseCount := 0
	for _, v := range items {
		inUseCount += kit.Ternary(v.InUse, 1, 0)
	}
	counts := types.VolumeUsageCounts{
		Inuse:  inUseCount,
		Unused: len(items) - inUseCount,
		Total:  len(items),
	}
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, effectiveParams)
	slog.DebugContext(
		ctx, "volume service: listed volumes",
		"dockerHost", s.dockerService.DockerHost(),
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

func (s *VolumeService) AggregateVolumes(ctx context.Context, params pagination.QueryParams) (types.PaginatedWithCounts[types.Volume, VolumeUsageCountsData], error) {
	collection, err := httpx.CollectEnvironments[types.PaginatedWithCounts[types.Volume, VolumeUsageCountsData]](ctx, url.Values{"start": {"0"}, "limit": {"-1"}})
	if err != nil {
		return types.PaginatedWithCounts[types.Volume, VolumeUsageCountsData]{}, err
	}

	pages := make([]pagination.EnvironmentPage[types.Volume], 0, len(collection.Results))
	counts := VolumeUsageCountsData{}
	for _, result := range collection.Results {
		page := result.Value
		pages = append(pages, pagination.EnvironmentPage[types.Volume]{
			Environment:    result.Environment,
			Items:          page.Data,
			TotalAvailable: cmp.Or(page.Pagination.GrandTotalItems, page.Pagination.TotalItems),
		})
		counts.Inuse += page.Counts.Inuse
		counts.Unused += page.Counts.Unused
		counts.Total += page.Counts.Total
	}
	data, resp := pagination.MergeEnvironmentPages(s.buildVolumePaginationConfig(), pages, params)

	return types.PaginatedWithCounts[types.Volume, VolumeUsageCountsData]{
		Success:            true,
		Data:               data,
		Counts:             counts,
		Pagination:         handlerutil.PaginationResponse(resp),
		FailedEnvironments: collection.Failures,
	}, nil
}

func (s *VolumeService) AggregateVolumeCounts(ctx context.Context) (types.ApiResponse[VolumeUsageCountsData], error) {
	return httpx.CollectEnvironmentData(ctx, nil, func(environments []types.EnvironmentData[VolumeUsageCountsData]) VolumeUsageCountsData {
		var counts VolumeUsageCountsData
		for _, environment := range environments {
			counts = VolumeUsageCountsData{
				Inuse:  counts.Inuse + environment.Data.Inuse,
				Unused: counts.Unused + environment.Data.Unused,
				Total:  counts.Total + environment.Data.Total,
			}
		}
		return counts
	})
}

func (s *VolumeService) AggregateVolumeSizes(ctx context.Context) (types.ApiResponse[[]VolumeSizeInfo], error) {
	collection, err := httpx.CollectEnvironments[types.ApiResponse[[]VolumeSizeInfo]](ctx, nil)
	if err != nil {
		return types.ApiResponse[[]VolumeSizeInfo]{}, err
	}

	sizes := make([]VolumeSizeInfo, 0)
	for _, result := range collection.Results {
		for _, size := range result.Value.Data {
			size.SetEnvironment(result.Environment)
			sizes = append(sizes, size)
		}
	}

	return types.ApiResponse[[]VolumeSizeInfo]{Success: true, Data: sizes, FailedEnvironments: collection.Failures}, nil
}

// RenameVolume copies an unused volume to a new name and removes the source.
func (s *VolumeService) RenameVolume(ctx context.Context, oldName, newName string, user types.Actor) (*types.Volume, error) {
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
		s.logVolumeRenameError(ctx, oldName, newName, user, "connect", err)
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	source, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeInspectResult, error) {
		return dockerClient.VolumeInspect(ctx, oldName, client.VolumeInspectOptions{})
	})
	if err != nil {
		if errdefs.IsNotFound(err) {
			err = common.Classify(common.ErrNotFound, err)
		}
		s.logVolumeRenameError(ctx, oldName, newName, user, "inspect", err)
		return nil, fmt.Errorf("inspect source volume: %w", err)
	}
	if s.isInternalVolume(types.NewVolume(source.Volume)) {
		return nil, common.ErrVolumeRenameProtected
	}

	// Stop any read-only browse helper first; a helper mounting the volume would
	// otherwise fail the detached-source check with "in use".
	if stopErr := s.StopHelper(ctx, oldName); stopErr != nil {
		slog.WarnContext(ctx, "could not stop volume browse helper before rename", "volume", oldName, "error", stopErr.Error())
	}

	migration, err := volumes.PlanRename(ctx, dockerClient, oldName, newName, s.toolsImage())
	if err != nil {
		s.logVolumeRenameError(ctx, oldName, newName, user, "plan", err)
		return nil, err
	}
	if applyErr := migration.Apply(ctx); applyErr != nil {
		if errdefs.IsInvalidArgument(applyErr) {
			applyErr = common.Classify(common.ErrBadRequest, applyErr)
		}
		s.logVolumeRenameError(ctx, oldName, newName, user, "apply", applyErr)
		return nil, applyErr
	}

	if renameVolumeMetadataErr := s.renameVolumeMetadata(ctx, oldName, newName); renameVolumeMetadataErr != nil {
		rollbackErr := migration.Rollback(ctx)
		combinedErr := errors.Join(fmt.Errorf("rename volume metadata: %w", renameVolumeMetadataErr), rollbackErr)
		s.logVolumeRenameError(ctx, oldName, newName, user, "metadata", combinedErr)
		return nil, combinedErr
	}

	if committer, ok := migration.(types.VolumeRenameCommitter); ok {
		if commitErr := committer.Commit(ctx); commitErr != nil {
			if _, localOk := errors.AsType[*types.SourceCleanupError](commitErr); localOk {
				// The copy and metadata are committed; only removing the source
				// failed, so the rename itself succeeded.
				slog.WarnContext(ctx, "volume renamed but source volume could not be removed", "oldVolume", oldName, "newVolume", newName, "error", commitErr.Error())
			} else {
				metadataErr := s.renameVolumeMetadata(ctx, newName, oldName)
				rollbackErr := migration.Rollback(ctx)
				combinedErr := errors.Join(commitErr, metadataErr, rollbackErr)
				s.logVolumeRenameError(ctx, oldName, newName, user, "commit", combinedErr)
				return nil, combinedErr
			}
		}
	}

	s.takeHelperID(oldName)
	docker.InvalidateVolumeUsageCache(dockerClient)

	renamed, err := s.GetVolumeByName(ctx, newName)
	if err != nil {
		s.logVolumeRenameError(ctx, oldName, newName, user, "inspect-renamed", err)
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
func (s *VolumeService) renameVolumeMetadata(ctx context.Context, oldName, newName string) error {
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

func (s *VolumeService) logVolumeRenameError(ctx context.Context, oldName, newName string, user types.Actor, step string, err error) {
	s.eventService.LogErrorEvent(ctx, event.EventTypeVolumeError, "volume", oldName, oldName, user.ID, user.Username, "0", err, database.JSON{
		"action":  "rename",
		"step":    step,
		"oldName": oldName,
		"newName": newName,
	})
}
