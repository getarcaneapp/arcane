package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/dto"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/docker"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
)

type VolumeService struct {
	db            *database.DB
	dockerService *DockerClientService
	eventService  *EventService
}

func NewVolumeService(db *database.DB, dockerService *DockerClientService, eventService *EventService) *VolumeService {
	return &VolumeService{
		db:            db,
		dockerService: dockerService,
		eventService:  eventService,
	}
}

func (s *VolumeService) GetVolumeByName(ctx context.Context, name string) (*dto.VolumeDto, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	vol, err := dockerClient.VolumeInspect(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("volume not found: %w", err)
	}

	if usageVolumes, duErr := docker.GetVolumeUsageData(ctx, dockerClient); duErr == nil {
		for _, uv := range usageVolumes {
			if uv.Name == vol.Name && uv.UsageData != nil {
				vol.UsageData = uv.UsageData
				slog.DebugContext(ctx, "attached volume usage data",
					slog.String("volume", vol.Name),
					slog.Int64("size_bytes", uv.UsageData.Size),
					slog.Int64("ref_count", uv.UsageData.RefCount))
				break
			}
		}
	} else {
		slog.WarnContext(ctx, "failed to load volume usage data",
			slog.String("volume", vol.Name),
			slog.String("error", duErr.Error()))
	}

	v := dto.NewVolumeDto(vol)

	containerIDs, err := docker.GetContainersUsingVolume(ctx, dockerClient, name)
	if err != nil {
		slog.WarnContext(ctx, "failed to get containers using volume",
			slog.String("volume", name),
			slog.String("error", err.Error()))
	} else {
		v.Containers = containerIDs
		if len(containerIDs) > 0 {
			v.InUse = true
		}
	}

	return &v, nil
}

func (s *VolumeService) CreateVolume(ctx context.Context, options volume.CreateOptions, user models.User) (*dto.VolumeDto, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", "", options.Name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	created, err := dockerClient.VolumeCreate(ctx, options)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", "", options.Name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to create volume: %w", err)
	}

	vol, err := dockerClient.VolumeInspect(ctx, created.Name)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", created.Name, created.Name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver, "step": "inspect"})
		return nil, fmt.Errorf("failed to inspect created volume: %w", err)
	}

	metadata := models.JSON{
		"action": "create",
		"driver": vol.Driver,
		"name":   vol.Name,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeCreate, vol.Name, vol.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume creation action",
			slog.String("volume", vol.Name),
			slog.String("error", logErr.Error()))
	}

	docker.InvalidateVolumeUsageCache()

	dtoVol := dto.NewVolumeDto(vol)
	return &dtoVol, nil
}

func (s *VolumeService) DeleteVolume(ctx context.Context, name string, force bool, user models.User) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", name, name, user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if err := dockerClient.VolumeRemove(ctx, name, force); err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", name, name, user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force})
		return fmt.Errorf("failed to remove volume: %w", err)
	}

	metadata := models.JSON{
		"action": "delete",
		"name":   name,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeDelete, name, name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume deletion action",
			slog.String("volume", name),
			slog.String("error", logErr.Error()))
	}

	return nil
}

func (s *VolumeService) PruneVolumes(ctx context.Context) (*dto.VolumePruneReportDto, error) {
	return s.PruneVolumesWithOptions(ctx, false)
}

func (s *VolumeService) PruneVolumesWithOptions(ctx context.Context, all bool) (*dto.VolumePruneReportDto, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Docker's VolumesPrune behavior (API v1.42+):
	// - Without 'all' flag: Only removes anonymous (unnamed) volumes that are not in use
	// - With 'all=true' flag: Removes ALL unused volumes (both named and anonymous)
	// Note: Volumes are considered "in use" if referenced by any container (running or stopped)
	filterArgs := filters.NewArgs()
	if all {
		// The 'all' filter was added in Docker API v1.42
		// This tells Docker to prune ALL unused volumes, not just anonymous ones
		filterArgs.Add("all", "true")
	}
	// Other valid filters for volume prune:
	// - label=<key> or label=<key>=<value>
	// - label!=<key> or label!=<key>=<value>

	report, err := dockerClient.VolumesPrune(ctx, filterArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to prune volumes: %w", err)
	}

	metadata := models.JSON{
		"action":         "prune",
		"all":            all,
		"volumesDeleted": len(report.VolumesDeleted),
		"spaceReclaimed": report.SpaceReclaimed,
	}
	if logErr := s.eventService.LogVolumeEvent(ctx, models.EventTypeVolumeDelete, "", "bulk_prune", systemUser.ID, systemUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log volume prune action",
			slog.String("error", logErr.Error()))
	}

	docker.InvalidateVolumeUsageCache()

	return &dto.VolumePruneReportDto{
		VolumesDeleted: report.VolumesDeleted,
		SpaceReclaimed: report.SpaceReclaimed,
	}, nil
}

func (s *VolumeService) GetVolumeUsage(ctx context.Context, name string) (bool, []string, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return false, nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	vol, err := dockerClient.VolumeInspect(ctx, name)
	if err != nil {
		return false, nil, fmt.Errorf("volume not found: %w", err)
	}

	containerIDs, err := docker.GetContainersUsingVolume(ctx, dockerClient, vol.Name)
	if err != nil {
		return false, nil, fmt.Errorf("failed to get containers using volume: %w", err)
	}

	inUse := len(containerIDs) > 0
	return inUse, containerIDs, nil
}

func (s *VolumeService) enrichVolumesWithUsageData(volumes []*volume.Volume, usageVolumes []volume.Volume) []volume.Volume {
	result := make([]volume.Volume, 0, len(volumes))
	for _, v := range volumes {
		if v != nil {
			for _, uv := range usageVolumes {
				if uv.Name == v.Name && uv.UsageData != nil {
					v.UsageData = uv.UsageData
					break
				}
			}

			result = append(result, *v)
		}
	}
	return result
}

func (s *VolumeService) buildVolumeContainerMap(ctx context.Context, dockerClient *client.Client) (map[string][]string, error) {
	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: true})
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

func (s *VolumeService) buildVolumePaginationConfig() pagination.Config[dto.VolumeDto] {
	return pagination.Config[dto.VolumeDto]{
		SearchAccessors: []pagination.SearchAccessor[dto.VolumeDto]{
			func(v dto.VolumeDto) (string, error) { return v.Name, nil },
			func(v dto.VolumeDto) (string, error) { return v.Driver, nil },
			func(v dto.VolumeDto) (string, error) { return v.Mountpoint, nil },
			func(v dto.VolumeDto) (string, error) { return v.Scope, nil },
		},
		SortBindings:    s.buildVolumeSortBindings(),
		FilterAccessors: s.buildVolumeFilterAccessors(),
	}
}

func (s *VolumeService) buildVolumeSortBindings() []pagination.SortBinding[dto.VolumeDto] {
	return []pagination.SortBinding[dto.VolumeDto]{
		{
			Key: "name",
			Fn:  func(a, b dto.VolumeDto) int { return strings.Compare(a.Name, b.Name) },
		},
		{
			Key: "driver",
			Fn:  func(a, b dto.VolumeDto) int { return strings.Compare(a.Driver, b.Driver) },
		},
		{
			Key: "mountpoint",
			Fn:  func(a, b dto.VolumeDto) int { return strings.Compare(a.Mountpoint, b.Mountpoint) },
		},
		{
			Key: "scope",
			Fn:  func(a, b dto.VolumeDto) int { return strings.Compare(a.Scope, b.Scope) },
		},
		{
			Key: "created",
			Fn:  func(a, b dto.VolumeDto) int { return strings.Compare(a.CreatedAt, b.CreatedAt) },
		},
		{
			Key: "inUse",
			Fn: func(a, b dto.VolumeDto) int {
				if a.InUse == b.InUse {
					return 0
				}
				if a.InUse {
					return -1
				}
				return 1
			},
		},
		{
			Key: "size",
			Fn:  s.compareVolumeSizes,
		},
	}
}

func (s *VolumeService) compareVolumeSizes(a, b dto.VolumeDto) int {
	aSize := int64(-1)
	bSize := int64(-1)
	if a.UsageData != nil {
		aSize = a.UsageData.Size
	}
	if b.UsageData != nil {
		bSize = b.UsageData.Size
	}
	if aSize == bSize {
		return 0
	}
	if aSize < bSize {
		return -1
	}
	return 1
}

func (s *VolumeService) buildVolumeFilterAccessors() []pagination.FilterAccessor[dto.VolumeDto] {
	return []pagination.FilterAccessor[dto.VolumeDto]{
		{
			Key: "inUse",
			Fn: func(v dto.VolumeDto, filterValue string) bool {
				if filterValue == "true" {
					return v.InUse
				}
				if filterValue == "false" {
					return !v.InUse
				}
				return true
			},
		},
	}
}

func (s *VolumeService) calculateVolumeUsageCounts(items []dto.VolumeDto) dto.VolumeUsageCounts {
	counts := dto.VolumeUsageCounts{
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

func (s *VolumeService) buildPaginationResponse(result pagination.FilterResult[dto.VolumeDto], params pagination.QueryParams) pagination.Response {
	totalPages := int64(0)
	if params.Limit > 0 {
		totalPages = (int64(result.TotalCount) + int64(params.Limit) - 1) / int64(params.Limit)
	}

	page := 1
	if params.Limit > 0 {
		page = (params.Start / params.Limit) + 1
	}

	return pagination.Response{
		TotalPages:      totalPages,
		TotalItems:      int64(result.TotalCount),
		CurrentPage:     page,
		ItemsPerPage:    params.Limit,
		GrandTotalItems: int64(result.TotalAvailable),
	}
}

func (s *VolumeService) ListVolumesPaginated(ctx context.Context, params pagination.QueryParams) ([]dto.VolumeDto, pagination.Response, dto.VolumeUsageCounts, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, dto.VolumeUsageCounts{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	volListBody, err := dockerClient.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, pagination.Response{}, dto.VolumeUsageCounts{}, fmt.Errorf("failed to list Docker volumes: %w", err)
	}

	usageVolumes, duErr := docker.GetVolumeUsageData(ctx, dockerClient)
	if duErr != nil {
		slog.WarnContext(ctx, "failed to load volume usage data",
			slog.String("error", duErr.Error()))
		usageVolumes = nil
	}

	volumes := s.enrichVolumesWithUsageData(volListBody.Volumes, usageVolumes)

	volumeContainerMap, err := s.buildVolumeContainerMap(ctx, dockerClient)
	if err != nil {
		slog.WarnContext(ctx, "failed to build volume-container map",
			slog.String("error", err.Error()))
		volumeContainerMap = make(map[string][]string)
	}

	items := make([]dto.VolumeDto, 0, len(volumes))
	for _, v := range volumes {
		volDto := dto.NewVolumeDto(v)
		if containerIDs, ok := volumeContainerMap[v.Name]; ok {
			volDto.Containers = containerIDs
			if len(containerIDs) > 0 {
				volDto.InUse = true
			}
		}
		items = append(items, volDto)
	}

	config := s.buildVolumePaginationConfig()
	result := pagination.SearchOrderAndPaginate(items, params, config)
	counts := s.calculateVolumeUsageCounts(items)
	paginationResp := s.buildPaginationResponse(result, params)

	return result.Items, paginationResp, counts, nil
}
