package volume

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/rustic"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
	"golang.org/x/sync/singleflight"
)

type VolumeService struct {
	db               *database.DB
	dockerService    *docker.DockerClientService
	eventService     *event.EventService
	activityService  *activity.ActivityService
	settingsService  *settings.SettingsService
	containerService *container.ContainerService
	imageService     *image.ImageService
	rusticService    *rustic.RusticService
	s3Destinations   *s3domain.S3DestinationService
	backupVolumeName string
	encryptionKey    string
	helperMu         sync.Mutex
	helperByVolume   map[string]*volumeHelper
	// helperGroup deduplicates concurrent read-only helper creation per volume.
	// Without it two simultaneous browse requests each create a helper and the
	// second overwrites the first in helperByVolume, orphaning a `sleep infinity`
	// container that pins the volume until restart.
	helperGroup    singleflight.Group
	scheduler      schedulertypes.DynamicScheduler
	lifecycleCtx   context.Context
	runningBackups sync.Map
}

const trivyCacheVolumePruneFilterValue = libarcane.InternalResourceLabel + "=true"

func NewVolumeService(db *database.DB, dockerService *docker.DockerClientService, eventService *event.EventService, activityService *activity.ActivityService, settingsService *settings.SettingsService, containerService *container.ContainerService, imageService *image.ImageService, rusticService *rustic.RusticService, s3Destinations *s3domain.S3DestinationService, backupVolumeName, encryptionKey string) *VolumeService {
	slog.Debug("volume service: new")
	if strings.TrimSpace(backupVolumeName) == "" {
		backupVolumeName = "arcane-backups"
	}
	return &VolumeService{
		db:               db,
		dockerService:    dockerService,
		eventService:     eventService,
		activityService:  activityService,
		settingsService:  settingsService,
		containerService: containerService,
		imageService:     imageService,
		rusticService:    rusticService,
		s3Destinations:   s3Destinations,
		backupVolumeName: backupVolumeName,
		encryptionKey:    encryptionKey,
		helperByVolume:   make(map[string]*volumeHelper),
	}
}

func (s *VolumeService) GetVolumeByName(ctx context.Context, name string) (*volumetypes.Volume, error) {
	slog.DebugContext(ctx, "volume service: get volume", "volume", name)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	volResult, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err != nil {
		return nil, errors.WrapIf(err, "volume not found")
	}
	vol := volResult.Volume

	settings := s.settingsService.GetSettingsConfig()
	usageCtx, usageCancel := context.WithTimeout(ctx, timeouts.GetDuration(settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI))
	defer usageCancel()
	if usageVolumes, ok := dockerutil.GetVolumeUsageDataStaleWhileRevalidate(usageCtx, dockerClient).Get(); ok {
		for _, uv := range usageVolumes {
			if uv.Name == vol.Name && uv.UsageData != nil {
				vol.UsageData = uv.UsageData
				slog.DebugContext(ctx, "attached volume usage data", "volume", vol.Name, "size_bytes", uv.UsageData.Size, "ref_count", uv.UsageData.RefCount)
				break
			}
		}
	}

	v := volumetypes.NewSummary(vol)

	containerIDs, err := dockerutil.GetContainersUsingVolume(ctx, dockerClient, name)
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

func (s *VolumeService) CreateVolume(ctx context.Context, options client.VolumeCreateOptions, user models.User) (*volumetypes.Volume, error) {
	slog.DebugContext(ctx, "volume service: create volume", "volume", options.Name, "driver", options.Driver, "user", user.ID)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", "", options.Name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver})
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	created, err := dockerClient.VolumeCreate(ctx, options)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", "", options.Name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver})
		return nil, errors.WrapIf(err, "failed to create volume")
	}

	vol, err := dockerClient.VolumeInspect(ctx, created.Volume.Name, client.VolumeInspectOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", created.Volume.Name, created.Volume.Name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver, "step": "inspect"})
		return nil, errors.WrapIf(err, "failed to inspect created volume")
	}

	metadata := models.JSON{
		"action": "create",
		"driver": vol.Volume.Driver,
		"name":   vol.Volume.Name,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeCreate, vol.Volume.Name, vol.Volume.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume creation action", "volume", vol.Volume.Name, "error", logErr.Error())
	}

	dockerutil.InvalidateVolumeUsageCache(dockerClient)

	return new(volumetypes.NewSummary(vol.Volume)), nil
}

func (s *VolumeService) DeleteVolume(ctx context.Context, name string, force bool, user models.User) error {
	slog.DebugContext(ctx, "volume service: delete volume", "volume", name, "force", force, "user", user.ID)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", name, name, user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force})
		return errors.WrapIf(err, "failed to connect to Docker")
	}

	// Stop any read-only browse helper first; a helper mounting the volume would
	// otherwise block a non-forced VolumeRemove with "volume is in use".
	if stopErr := s.StopHelper(ctx, name); stopErr != nil {
		slog.WarnContext(ctx, "could not stop volume browse helper before delete", "volume", name, "error", stopErr.Error())
	}

	if _, err := dockerClient.VolumeRemove(ctx, name, client.VolumeRemoveOptions{
		Force: force,
	}); err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", name, name, user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force})
		return errors.WrapIf(err, "failed to remove volume")
	}

	metadata := models.JSON{
		"action": "delete",
		"name":   name,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeDelete, name, name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume deletion action", "volume", name, "error", logErr.Error())
	}

	s.removeHelperEntry(name)
	s.removeVolumeBackupPolicyInternal(ctx, name)
	dockerutil.InvalidateVolumeUsageCache(dockerClient)
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
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	// Stop all read-only browse helpers first; a helper mounting a volume marks it
	// "in use" and would prevent VolumePrune from reclaiming an otherwise-unused
	// volume. Helpers are re-created on demand on the next browse request.
	s.CleanupHelperContainers(ctx)

	preserveTrivyCache := s.preserveTrivyCacheOnVolumePruneInternal()

	// Docker's VolumesPrune behavior (API v1.42+):
	// - Without 'all' flag: Only removes anonymous (unnamed) volumes that are not in use
	// - With 'all=true' flag: Removes ALL unused volumes (both named and anonymous)
	// Note: Volumes are considered "in use" if referenced by any container (running or stopped)
	volumePruneOptions := buildVolumePruneOptionsInternal(all, preserveTrivyCache)
	volumePruneResult, err := dockerClient.VolumePrune(ctx, volumePruneOptions)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to prune volumes")
	}

	metadata := buildVolumePruneMetadataInternal(all, len(volumePruneResult.Report.VolumesDeleted), volumePruneResult.Report.SpaceReclaimed, preserveTrivyCache)
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeDelete, "", "bulk_prune", models.SystemUser.ID, models.SystemUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume prune action", "error", logErr.Error())
	}

	for _, volumeName := range volumePruneResult.Report.VolumesDeleted {
		s.removeHelperEntry(volumeName)
		s.removeVolumeBackupPolicyInternal(ctx, volumeName)
	}

	dockerutil.InvalidateVolumeUsageCache(dockerClient)

	return &volumetypes.PruneReport{
		VolumesDeleted: volumePruneResult.Report.VolumesDeleted,
		SpaceReclaimed: volumePruneResult.Report.SpaceReclaimed,
	}, nil
}

func (s *VolumeService) preserveTrivyCacheOnVolumePruneInternal() bool {
	if s.settingsService == nil {
		return true
	}

	return s.settingsService.GetSettingsConfig().TrivyPreserveCacheOnVolumePrune.IsTrue()
}

func buildVolumePruneOptionsInternal(all, preserveTrivyCache bool) client.VolumePruneOptions {
	options := client.VolumePruneOptions{
		All: all,
	}
	if !preserveTrivyCache {
		return options
	}

	filters := make(client.Filters)
	filters = filters.Add("label!", trivyCacheVolumePruneFilterValue)
	options.Filters = filters

	return options
}

func buildVolumePruneMetadataInternal(all bool, volumesDeleted int, spaceReclaimed uint64, preserveTrivyCache bool) models.JSON {
	return models.JSON{
		"action":                "prune",
		"all":                   all,
		"volumesDeleted":        volumesDeleted,
		"spaceReclaimed":        spaceReclaimed,
		"preserveTrivyCache":    preserveTrivyCache,
		"trivyCacheFilterLabel": trivyCacheVolumePruneFilterValue,
	}
}
