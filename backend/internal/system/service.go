package system

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/getarcaneapp/arcane/types/v2/dockerinfo"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/system"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	mobycontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/samber/hot"
	"github.com/samber/mo"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/labels"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/network"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	systembackup "github.com/getarcaneapp/arcane/backend/v2/internal/system/children/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system/children/prune"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system/children/upgrade"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/vuln"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
)

// SystemService owns Docker system-wide operations and coordinates the prune
// and self-upgrade features.
type SystemService struct {
	db                    *database.DB
	dockerService         *dockerInternal.DockerClientService
	containerService      *container.ContainerService
	settingsService       *settings.SettingsService
	activityService       *activity.ActivityService
	dockerHostMemoryCache *hot.HotCache[struct{}, dockerHostMemoryInfo]

	prune   *prune.Service
	upgrade *upgrade.Service
	backup  *systembackup.Service
}

func NewSystemService(
	db *database.DB,
	dockerService *dockerInternal.DockerClientService,
	containerService *container.ContainerService,
	imageUpdateService *imageupdate.ImageUpdateService,
	volumeService *volume.VolumeService,
	networkService *network.NetworkService,
	settingsService *settings.SettingsService,
	activityService *activity.ActivityService,
	versionService *version.VersionService,
	eventService *event.EventService,
	projectService *project.ProjectService,
	registryService *registry.ContainerRegistryService,
	backupEngine *backup.Engine,
	s3Destinations *s3.S3DestinationService,
	recoveryKeys *backup.RecoveryKeyStore,
	actors *francis.Runtime,
	cfg *config.Config,
) *SystemService {
	s := &SystemService{
		db:               db,
		dockerService:    dockerService,
		containerService: containerService,
		settingsService:  settingsService,
		activityService:  activityService,
		dockerHostMemoryCache: hot.NewHotCache[struct{}, dockerHostMemoryInfo](hot.LRU, 1).
			WithTTL(dockerHostMemoryCacheTTL).
			Build(),
		prune:   prune.NewService(cfg.GetLocation(), dockerService, activityService, imageUpdateService, volumeService, networkService),
		upgrade: upgrade.NewService(db, dockerService, versionService, eventService, settingsService, projectService, registryService, resolveUpgraderRuntimeOptions),
	}
	s.backup = systembackup.NewService(systembackup.Dependencies{
		Store:                 s.backupStore(s3Destinations),
		DB:                    db,
		SQLDB:                 db.SQLDB,
		ActorStore:            actors.Store,
		Docker:                dockerService,
		Volumes:               volumeService,
		Engine:                backupEngine,
		S3Destinations:        s3Destinations,
		Activity:              activityService,
		Settings:              settingsService,
		Config:                cfg,
		RecoveryKeys:          recoveryKeys,
		ResolveRuntimeOptions: resolveUpgraderRuntimeOptions,
	})
	return s
}

// RegisterWorkflows defines the backup workflows while the host is still unstarted.
func (s *SystemService) RegisterWorkflows(engine *flow.Engine, environments *environment.EnvironmentService, roles *role.RoleService) error {
	return errors.Join(s.backup.RegisterWorkflows(engine), s.upgrade.RegisterWorkflows(engine, environments, roles))
}

// ReconcileInterruptedBackups fails system backup runs left running by the previous process and watches the
// manual backups that saved workflows resume.
func (s *SystemService) ReconcileInterruptedBackups(ctx context.Context, protectedIDs ...string) error {
	query := s.db.WithContext(ctx).Model(&SystemBackupRun{}).Where("status = ?", SystemBackupStatusRunning)
	if len(protectedIDs) > 0 {
		query = query.Where("id NOT IN ?", protectedIDs)
	}
	if err := query.Updates(map[string]any{"status": SystemBackupStatusFailed, "error": "Backup interrupted by application restart"}).Error; err != nil {
		return err
	}
	return s.backup.WatchAcceptedBackups(ctx)
}

// SetBackupScheduler injects the dynamic scheduler and admission gate for
// per-policy system backup jobs. Agent mode leaves them unset.
func (s *SystemService) SetBackupScheduler(ctx context.Context, dynamicScheduler scheduler.DynamicScheduler, admissionGate *runs.Admission) error {
	return s.backup.SetScheduler(ctx, dynamicScheduler, admissionGate)
}

// PruneLocalBackupRepository frees the space of deleted local system backups.
func (s *SystemService) PruneLocalBackupRepository(ctx context.Context) error {
	return s.backup.PruneLocalRepository(ctx)
}

// RegisterBackupJobOnStartup schedules every saved system and system-managed volume backup policy.
func (s *SystemService) RegisterBackupJobOnStartup(ctx context.Context) {
	s.backup.RegisterBackupJobOnStartup(ctx)
}

// PruneScheduled runs the scheduled prune job.
func (s *SystemService) PruneScheduled(ctx context.Context, environmentID string, req system.PruneAllRequest) (*system.PruneAllResult, bool, error) {
	return s.prune.PruneScheduled(ctx, environmentID, req)
}

// ReconcileScheduledPrune recovers an interrupted scheduled prune.
func (s *SystemService) ReconcileScheduledPrune(ctx context.Context, previous scheduler.Run) (scheduler.Outcome, error) {
	return s.prune.ReconcileScheduledPrune(ctx, previous)
}

// TriggerUpgradeViaCLI starts the self-upgrade for the updater engine.
func (s *SystemService) TriggerUpgradeViaCLI(ctx context.Context, user usertypes.Actor, target updater.SelfUpdateTarget) (string, error) {
	return s.upgrade.TriggerUpgradeViaCLI(ctx, user, target)
}

// ResumeUpdateAllOnStartup finalizes an update-all job interrupted by the manager restart.
func (s *SystemService) ResumeUpdateAllOnStartup(ctx context.Context) {
	s.upgrade.ResumeUpdateAllOnStartup(ctx)
}

// PruneUpgradeLogs removes expired upgrade logs.
func (s *SystemService) PruneUpgradeLogs(ctx context.Context, dataDir string, now time.Time) (int, error) {
	return s.upgrade.PruneUpgradeLogs(ctx, dataDir, now)
}

func (s *SystemService) StartAllContainers(ctx context.Context, environmentID string) (*containertypes.ActionResult, error) {
	return s.performBatchContainerAction(ctx, environmentID, startMatchingContainersOptions{
		ActivityType:   activitytypes.TypeContainerStart,
		ActionName:     "start",
		ResourceName:   "All containers",
		StartMessage:   "Starting all containers",
		SuccessMessage: "Started all containers",
		ShouldStart: func(c mobycontainer.Summary) bool {
			return (c.State == mobycontainer.StateExited || c.State == mobycontainer.StateCreated) &&
				!strings.EqualFold(c.Labels[libarcane.InternalResourceLabel], "true")
		},
		Action: func(ctx context.Context, id string) error {
			return s.containerService.StartContainer(ctx, id, usertypes.SystemUser)
		},
	})
}

func (s *SystemService) StartAllStoppedContainers(ctx context.Context, environmentID string) (*containertypes.ActionResult, error) {
	return s.performBatchContainerAction(ctx, environmentID, startMatchingContainersOptions{
		ActivityType:   activitytypes.TypeContainerStart,
		ActionName:     "start",
		ResourceName:   "Stopped containers",
		StartMessage:   "Starting stopped containers",
		SuccessMessage: "Started stopped containers",
		ShouldStart: func(c mobycontainer.Summary) bool {
			return c.State == mobycontainer.StateExited && !strings.EqualFold(c.Labels[libarcane.InternalResourceLabel], "true")
		},
		Action: func(ctx context.Context, id string) error {
			return s.containerService.StartContainer(ctx, id, usertypes.SystemUser)
		},
	})
}

func (s *SystemService) StopAllContainers(ctx context.Context, environmentID string) (*containertypes.ActionResult, error) {
	return s.performBatchContainerAction(ctx, environmentID, startMatchingContainersOptions{
		ActivityType:   activitytypes.TypeContainerStop,
		ActionName:     "stop",
		ResourceName:   "All containers",
		StartMessage:   "Stopping all containers",
		SuccessMessage: "Stopped all containers",
		ShouldStart: func(c mobycontainer.Summary) bool {
			return !labels.IsArcaneContainer(c.Labels) && !strings.EqualFold(c.Labels[libarcane.InternalResourceLabel], "true")
		},
		Action: func(ctx context.Context, id string) error {
			return s.containerService.StopContainer(ctx, id, usertypes.SystemUser)
		},
	})
}

type startMatchingContainersOptions struct {
	ActivityType   activitytypes.Type
	ActionName     string
	ResourceName   string
	StartMessage   string
	SuccessMessage string
	ShouldStart    func(mobycontainer.Summary) bool
	Action         func(context.Context, string) error
}

func (s *SystemService) performBatchContainerAction(ctx context.Context, environmentID string, opts startMatchingContainersOptions) (*containertypes.ActionResult, error) {
	localActivity, err := s.activityService.StartActivity(ctx, activity.StartActivityRequest{
		EnvironmentID: environmentID,
		Type:          opts.ActivityType,
		ResourceType:  new("system"),
		ResourceName:  &opts.ResourceName,
		Step:          opts.StartMessage,
		LatestMessage: opts.StartMessage,
		Metadata:      database.JSON{"scope": opts.ResourceName},
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to start system container activity", "type", opts.ActivityType, "error", err)
	}
	activityID := kit.Ternary(err != nil, "", kit.FromPtr(localActivity).ID)
	ctx = s.activityService.Track(ctx, activityID)

	var mu sync.Mutex
	var processed, failed, errs []string
	containers, _, _, _, listErr := s.dockerService.GetAllContainers(ctx)
	if listErr != nil {
		errs = append(errs, fmt.Sprintf("Failed to list containers: %v", listErr))
	} else {
		g, groupCtx := errgroup.WithContext(ctx)
		// Limit concurrency to avoid overwhelming Docker daemon
		g.SetLimit(5)
		for _, c := range containers {
			if !opts.ShouldStart(c) {
				continue
			}
			g.Go(func() (workerErr error) {
				defer utils.RecoverToError(&workerErr, "system info worker")
				actionErr := opts.Action(groupCtx, c.ID)
				mu.Lock()
				defer mu.Unlock()
				if actionErr != nil {
					failed = append(failed, c.ID)
					errs = append(errs, fmt.Sprintf("Failed to %s container %s: %v", opts.ActionName, c.ID, actionErr))
					return nil
				}
				processed = append(processed, c.ID)
				return nil
			})
		}
		_ = g.Wait()
	}

	result := &containertypes.ActionResult{
		Success:    len(errs) == 0,
		Started:    kit.Ternary(opts.ActionName == "start", processed, nil),
		Stopped:    kit.Ternary(opts.ActionName == "start", nil, processed),
		Failed:     failed,
		Errors:     errs,
		ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
	}
	if activityID != "" {
		_, completeErr := s.activityService.CompleteActivity(
			utils.ActivityRuntimeContext(ctx, nil),
			activityID,
			kit.Ternary(len(errs) > 0, activitytypes.StatusFailed, activitytypes.StatusSuccess),
			kit.Ternary(len(errs) > 0, strings.Join(errs, "; "), opts.SuccessMessage),
			kit.Ternary(len(errs) > 0, new(strings.Join(errs, "; ")), nil),
		)
		if completeErr != nil {
			slog.DebugContext(ctx, "failed to complete system container activity", "activityId", activityID, "error", completeErr)
		}
	}
	return result, listErr
}

// PingDocker checks that the Docker daemon is responsive.
func (s *SystemService) PingDocker(ctx context.Context) error {
	_, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.PingResult, error) {
		return cli.Ping(ctx, client.PingOptions{})
	})
	return err
}

// GetDockerInfo returns Docker daemon version and system information.
func (s *SystemService) GetDockerInfo(ctx context.Context) (dockerinfo.Info, error) {
	serverVersion, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ServerVersionResult, error) {
		return cli.ServerVersion(ctx, client.ServerVersionOptions{})
	})
	if err != nil {
		return dockerinfo.Info{}, fmt.Errorf("failed to get Docker version: %w", err)
	}

	infoResult, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.SystemInfoResult, error) {
		return cli.Info(ctx, client.InfoOptions{})
	})
	if err != nil {
		return dockerinfo.Info{}, err
	}
	info := infoResult.Info

	// Outside Docker (e.g. LXC) the daemon may report the whole machine, so cap totals at the guest's cgroup budget.
	// Inside Docker, --cpus/--memory are operator limits and the daemon's totals already reflect the host.
	if !cgroup.IsDockerContainer() {
		if cgroupLimits, detectLimitsErr := cgroup.DetectLimits(); detectLimitsErr == nil {
			if limit := cgroupLimits.MemoryLimit; limit > 0 && (info.MemTotal == 0 || limit < info.MemTotal) {
				info.MemTotal = limit // keep: copying Docker's ~70-field system.Info as a literal is unreadable
			}
			if cgroupLimits.CPUCount > 0 && (info.NCPU == 0 || cgroupLimits.CPUCount < info.NCPU) {
				info.NCPU = cgroupLimits.CPUCount // keep: copying Docker's ~70-field system.Info as a literal is unreadable
			}
		}
	}

	var gitCommit, goVersion, buildTime string
	for _, component := range serverVersion.Components {
		for key, value := range component.Details {
			switch strings.ToLower(key) {
			case "gitcommit":
				gitCommit = cmp.Or(gitCommit, value)
			case "goversion":
				goVersion = cmp.Or(goVersion, value)
			case "buildtime":
				buildTime = cmp.Or(buildTime, value)
			}
		}
	}

	return dockerinfo.Info{
		Success:    true,
		APIVersion: serverVersion.APIVersion,
		GitCommit:  gitCommit,
		GoVersion:  goVersion,
		Os:         serverVersion.Os,
		Arch:       serverVersion.Arch,
		BuildTime:  buildTime,
		Info:       info,
	}, nil
}

func (s *SystemService) GetDiskUsagePath(ctx context.Context) string {
	return cmp.Or(s.settingsService.GetSettingsConfig().DiskUsagePath.Value, "/")
}

const dockerHostMemoryCacheTTL = 30 * time.Second

type dockerHostMemoryInfo struct {
	root  string
	total uint64
}

// GetDockerHostMemory reports enclosing guest memory when Docker shares its
// host's cgroup namespace. A false result leaves the caller's baseline intact.
func (s *SystemService) GetDockerHostMemory(ctx context.Context) (uint64, uint64, bool) {
	if runtime.GOOS != "linux" || !cgroup.IsDockerContainer() {
		return 0, 0, false
	}

	info, found, err := s.dockerHostMemoryCache.GetWithLoaders(struct{}{}, func(_ []struct{}) (map[struct{}]dockerHostMemoryInfo, error) {
		metadata, err := s.loadDockerHostMemory(ctx)
		if err != nil {
			slog.DebugContext(ctx, "Docker host memory accounting unavailable", "error", err)
		}
		// Cache failures too, without retaining expired metadata.
		return map[struct{}]dockerHostMemoryInfo{{}: metadata}, nil
	})
	if err != nil || !found || info.total == 0 || ctx.Err() != nil {
		return 0, 0, false
	}

	used, err := cgroup.MemoryUsage(info.root)
	if err != nil {
		return 0, 0, false
	}
	return min(used, info.total), info.total, true
}

func (s *SystemService) loadDockerHostMemory(ctx context.Context) (dockerHostMemoryInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	containerID, err := cgroup.CurrentContainerID()
	if err != nil {
		return dockerHostMemoryInfo{}, err
	}
	inspect, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerInspectResult, error) {
		return compat.ContainerInspectWithCompatibility(ctx, cli, containerID, client.ContainerInspectOptions{})
	})
	if err != nil {
		return dockerHostMemoryInfo{}, err
	}
	if !strings.HasPrefix(inspect.Container.ID, containerID) || kit.FromPtr(inspect.Container.HostConfig).CgroupnsMode != "host" {
		return dockerHostMemoryInfo{}, errors.New("current container does not share the Docker host cgroup namespace")
	}
	// Inspection expands hostname and mountinfo short IDs before cgroup validation.
	root, err := cgroup.DockerHostRoot(inspect.Container.ID)
	if err != nil {
		return dockerHostMemoryInfo{}, err
	}

	info, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.SystemInfoResult, error) {
		return cli.Info(ctx, client.InfoOptions{})
	})
	if err != nil {
		return dockerHostMemoryInfo{}, err
	}
	if info.Info.MemTotal <= 0 {
		return dockerHostMemoryInfo{}, errors.New("Docker host memory total is unavailable") //nolint:staticcheck // Preserve the existing error message.
	}
	return dockerHostMemoryInfo{root: root, total: uint64(info.Info.MemTotal)}, nil
}

// resolveUpgraderRuntimeOptions determines how a helper container reaches the Docker daemon.
func resolveUpgraderRuntimeOptions(
	ctx context.Context,
	dockerHost string,
	currentContainer *mobycontainer.InspectResponse,
	discoverHostPath func(context.Context, string) (string, error),
	isRunningInDocker func() bool,
	selectReachableNetwork func(context.Context, *mobycontainer.InspectResponse, string) string,
) ([]string, []mount.Mount, mobycontainer.NetworkMode, error) {
	scheme, socketPath, err := vuln.ParseDockerHost(dockerHost)
	if err != nil {
		return nil, nil, "", fmt.Errorf("resolve docker host %q: %w", dockerHost, err)
	}

	// The upgrader must reach the tcp DOCKER_HOST, so prefer a network the
	// daemon proxy is actually on over the plain auto heuristic (#3533).
	if scheme != "unix" && selectReachableNetwork != nil {
		return vuln.BuildDockerHostEnv(dockerHost), nil, mobycontainer.NetworkMode(selectReachableNetwork(ctx, currentContainer, dockerHost)), nil
	}
	if scheme != "unix" {
		return vuln.BuildDockerHostEnv(dockerHost), nil, mobycontainer.NetworkMode(docker.SelectAutoNetworkMode(currentContainer)), nil
	}

	socketSource, err := vuln.ResolveUnixSocketSource(
		ctx,
		socketPath,
		discoverHostPath,
		isRunningInDocker,
	)
	if err != nil {
		return nil, nil, "", fmt.Errorf("resolve unix socket source: %w", err)
	}

	return vuln.BuildDockerHostEnv(dockerHost), []mount.Mount{{
		Type:   mount.TypeBind,
		Source: socketSource,
		Target: socketPath,
	}}, "", nil
}

const systemBackupHistoryUnion = "SELECT id, size, created_at, status, trigger, destination, '' AS format, local_snapshot_id, " +
	"remote_snapshot_id, '' AS remote_instance_id, s3_destination_id, policy_id, error, 'system' AS type, 'system' AS " +
	"resource_type, 'Arcane' AS resource_name FROM system_backup_runs UNION ALL SELECT id, size, " +
	"created_at, status, trigger, destination, format, local_snapshot_id, remote_snapshot_id, COALESCE(remote_instance_id, '') AS remote_instance_id, " +
	"s3_destination_id, policy_id, error, CASE WHEN policy_id LIKE 'system-volume:%' THEN 'system' ELSE " +
	"'volume' END AS type, 'volume' AS resource_type, volume_name AS resource_name FROM volume_backups"

// backupStore persists system backup runs and policies for the backup child.
func (s *SystemService) backupStore(s3Destinations *s3.S3DestinationService) systembackup.Store {
	toModel := func(dto *backuptypes.SystemBackupRun) SystemBackupRun {
		return SystemBackupRun{ //exhaustruct:enforce
			BaseModel: database.BaseModel{
				ID:        dto.ID,
				CreatedAt: time.Time{},
				UpdatedAt: nil,
			},
			Size:              dto.Size,
			CreatedAt:         dto.CreatedAt,
			Status:            SystemBackupStatus(dto.Status),
			Trigger:           SystemBackupTrigger(dto.Trigger),
			Destination:       dto.Destination,
			LocalSnapshotID:   dto.LocalSnapshotID,
			RemoteSnapshotID:  dto.RemoteSnapshotID,
			S3DestinationID:   dto.S3DestinationID,
			S3DestinationName: dto.S3DestinationName,
			PolicyID:          dto.PolicyID,
			Error:             dto.Error,
		}
	}
	return systembackup.Store{
		CreateRun: func(ctx context.Context, dto *backuptypes.SystemBackupRun) error {
			run := toModel(dto)
			if err := s.db.WithContext(ctx).Create(&run).Error; err != nil {
				return err
			}
			*dto = backuptypes.SystemBackupRun{ //exhaustruct:enforce
				RemoteAvailable:   dto.RemoteAvailable,
				ActivityID:        dto.ActivityID,
				ID:                run.ID,
				Size:              dto.Size,
				CreatedAt:         run.CreatedAt,
				Status:            dto.Status,
				Trigger:           dto.Trigger,
				Destination:       dto.Destination,
				LocalSnapshotID:   dto.LocalSnapshotID,
				RemoteSnapshotID:  dto.RemoteSnapshotID,
				S3DestinationID:   dto.S3DestinationID,
				S3DestinationName: dto.S3DestinationName,
				PolicyID:          dto.PolicyID,
				Error:             dto.Error,
			}
			return nil
		},
		SaveRun: func(ctx context.Context, dto *backuptypes.SystemBackupRun) error {
			return s.db.WithContext(ctx).Save(new(toModel(dto))).Error
		},
		FailRun: func(ctx context.Context, id, message string) error {
			return s.db.WithContext(ctx).Model(&SystemBackupRun{}).Where("id = ?", id).Updates(map[string]any{"status": SystemBackupStatusFailed, "error": message}).Error
		},
		DeleteRun: func(ctx context.Context, id string) error {
			return s.db.WithContext(ctx).Where("id = ?", id).Delete(&SystemBackupRun{}).Error
		},
		Run: func(ctx context.Context, id string) (*backuptypes.SystemBackupRun, error) {
			var run SystemBackupRun
			if err := s.db.WithContext(ctx).Where("id = ?", id).First(&run).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, common.Classify(common.ErrNotFound, errors.New("system backup not found"))
				}
				return nil, err
			}
			return new(run.ToDTO()), nil
		},
		ListRuns: func(ctx context.Context, params pagination.QueryParams) ([]backuptypes.SystemBackupRun, pagination.Response, error) {
			var records []SystemBackupRun
			query := s.db.WithContext(ctx).Model(&SystemBackupRun{})
			if term := strings.TrimSpace(params.Search); term != "" {
				pattern := "%" + term + "%"
				query = query.Where("status LIKE ? OR trigger LIKE ? OR destination LIKE ? OR error LIKE ?", pattern, pattern, pattern, pattern)
			}
			response, err := pagination.PaginateAndSortDB(params, query, &records)
			if err != nil {
				return nil, pagination.Response{}, err
			}
			result := make([]backuptypes.SystemBackupRun, len(records))
			for i := range records {
				result[i] = records[i].ToDTO()
			}
			return result, response, nil
		},
		ExpiredRunIDs: func(ctx context.Context, policyID string, keep int) ([]string, error) {
			return backup.ExpiredRunIDs(ctx, s.db, "system_backup_runs", policyID, keep)
		},
		RunsByID: func(ctx context.Context, ids []string) ([]*backuptypes.SystemBackupRun, error) {
			var records []SystemBackupRun
			if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&records).Error; err != nil {
				return nil, err
			}
			result := make([]*backuptypes.SystemBackupRun, len(records))
			for i := range records {
				result[i] = new(records[i].ToDTO())
			}
			return result, nil
		},
		LatestPolicyRun: func(ctx context.Context, policyID string) (*backuptypes.SystemBackupRun, error) {
			var run SystemBackupRun
			err := s.db.WithContext(ctx).Where("policy_id = ?", policyID).Order("created_at DESC").First(&run).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			return new(run.ToDTO()), nil
		},
		RemoteSnapshotIDs: func(ctx context.Context, destinationID string) ([]string, error) {
			var ids []string
			err := s.db.WithContext(ctx).Model(&SystemBackupRun{}).
				Where("s3_destination_id = ? AND remote_snapshot_id <> ''", destinationID).
				Pluck("remote_snapshot_id", &ids).Error
			return ids, err
		},
		CountRuns: func(ctx context.Context) (int64, error) {
			var count int64
			err := s.db.WithContext(ctx).Model(&SystemBackupRun{}).Count(&count).Error
			return count, err
		},
		CountLocalRuns: func(ctx context.Context) (int64, error) {
			var count int64
			err := s.db.WithContext(ctx).Model(&SystemBackupRun{}).Where("local_snapshot_id <> ''").Count(&count).Error
			return count, err
		},
		CountVolumeRepositoryRuns: func(ctx context.Context) (int64, error) {
			var count int64
			err := s.db.WithContext(ctx).Model(&volume.VolumeBackup{}).Where("format = ?", volume.VolumeBackupFormatRustic).Count(&count).Error
			return count, err
		},
		CountRunningVolumeBackups: func(ctx context.Context) (int64, error) {
			var count int64
			err := s.db.WithContext(ctx).Model(&volume.VolumeBackup{}).Where("status = ?", volume.VolumeBackupStatusRunning).Count(&count).Error
			return count, err
		},
		Policies: func(ctx context.Context) ([]backuptypes.SystemBackupPolicy, error) {
			var policies []SystemBackupPolicy
			if err := s.db.WithContext(ctx).Order("created_at ASC").Find(&policies).Error; err != nil {
				return nil, fmt.Errorf("failed to load system backup policies: %w", err)
			}
			result := make([]backuptypes.SystemBackupPolicy, len(policies))
			for i := range policies {
				result[i] = policies[i].ToDTO()
			}
			return result, nil
		},
		Policy: func(ctx context.Context, id string) (*backuptypes.SystemBackupPolicy, error) {
			if strings.TrimSpace(id) == "" {
				return nil, nil
			}
			var policy SystemBackupPolicy
			err := s.db.WithContext(ctx).Where("id = ?", id).First(&policy).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			if err != nil {
				return nil, fmt.Errorf("failed to load system backup policy: %w", err)
			}
			return new(policy.ToDTO()), nil
		},
		ReconcilePolicies: func(
			ctx context.Context,
			updates []backuptypes.UpdateSystemBackupPolicy,
			build func(context.Context, *backuptypes.SystemBackupPolicy, backuptypes.UpdateSystemBackupPolicy) error,
			unregister func(context.Context, string),
			reschedule func(context.Context, *backuptypes.SystemBackupPolicy),
		) error {
			var existing []SystemBackupPolicy
			if err := s.db.WithContext(ctx).Order("created_at ASC").Find(&existing).Error; err != nil {
				return fmt.Errorf("failed to load system backup policies: %w", err)
			}
			return backup.PolicyReconciliation[SystemBackupPolicy, backuptypes.UpdateSystemBackupPolicy]{
				Domain:   "system",
				DB:       s.db,
				Existing: existing,
				ID:       func(policy *SystemBackupPolicy) string { return policy.ID },
				UpdateID: func(update backuptypes.UpdateSystemBackupPolicy) string { return update.ID },
				New:      func() SystemBackupPolicy { return SystemBackupPolicy{} },
				Build: func(ctx context.Context, policy *SystemBackupPolicy, update backuptypes.UpdateSystemBackupPolicy) error {
					dto := policy.ToDTO()
					if buildErr := build(ctx, &dto, update); buildErr != nil {
						return buildErr
					}
					*policy = SystemBackupPolicy{ //exhaustruct:enforce
						BaseModel:       policy.BaseModel,
						Enabled:         dto.Enabled,
						Schedule:        dto.Schedule,
						RetentionCount:  dto.RetentionCount,
						LocalEnabled:    dto.LocalEnabled,
						S3Enabled:       dto.S3Enabled,
						S3DestinationID: dto.S3DestinationID,
					}
					return nil
				},
				Unregister: unregister,
				Reschedule: func(ctx context.Context, policy *SystemBackupPolicy) {
					reschedule(ctx, new(policy.ToDTO()))
				},
			}.Run(ctx, updates)
		},
		DisablePolicyRemote: func(ctx context.Context, policy *backuptypes.SystemBackupPolicy, field string) (bool, error) {
			return backup.DisableStoredRemote(ctx, s.db, &SystemBackupPolicy{}, policy.ID, policy.S3DestinationID, policy.LocalEnabled, field)
		},
		CheckScheduledRemote: func(ctx context.Context, destinationID string) error {
			return backup.CheckScheduledRemote(ctx, s.db, s3Destinations, "system_backup_runs", destinationID, "arcane-system-recovery")
		},
		ListHistory: func(ctx context.Context, params pagination.QueryParams) ([]backuptypes.HistoryEntry, pagination.Response, error) {
			query := s.db.WithContext(ctx).Table("(?) AS backup_history", s.db.Raw(systemBackupHistoryUnion))
			if term := strings.TrimSpace(params.Search); term != "" {
				pattern := "%" + term + "%"
				query = query.Where("id LIKE ? OR status LIKE ? OR trigger LIKE ? OR destination LIKE ? OR COALESCE(error, '') LIKE ? OR "+
					"resource_name LIKE ? OR resource_type LIKE ? OR type LIKE ?",
					pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern,
				)
			}
			query = pagination.ApplyFilter(query, "type", params.Filters["type"])
			var history []backuptypes.HistoryEntry
			page, err := pagination.PaginateAndSortDB(pagination.QueryParams{
				SearchQuery: params.SearchQuery,
				SortParams: pagination.SortParams{
					Sort:  cmp.Or(params.Sort, "createdAt"),
					Order: kit.Ternary(params.Sort == "", pagination.SortDesc, params.Order),
				},
				Params:  params.Params,
				Filters: params.Filters,
			}, query, &history)
			return history, page, err
		},
	}
}
