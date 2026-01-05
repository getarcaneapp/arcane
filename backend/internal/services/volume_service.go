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
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/docker"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	volumetypes "github.com/getarcaneapp/arcane/types/volume"
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

func (s *VolumeService) GetVolumeByName(ctx context.Context, name string) (*volumetypes.Volume, error) {
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
				slog.DebugContext(ctx, "attached volume usage data", "volume", vol.Name, "size_bytes", uv.UsageData.Size, "ref_count", uv.UsageData.RefCount)
				break
			}
		}
	} else {
		slog.WarnContext(ctx, "failed to load volume usage data", "volume", vol.Name, "error", duErr.Error())
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

func (s *VolumeService) CreateVolume(ctx context.Context, options volume.CreateOptions, user models.User) (*volumetypes.Volume, error) {
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
		slog.WarnContext(ctx, "could not log volume creation action", "volume", vol.Name, "error", logErr.Error())
	}

	docker.InvalidateVolumeUsageCache()

	dtoVol := volumetypes.NewSummary(vol)
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
		slog.WarnContext(ctx, "could not log volume deletion action", "volume", name, "error", logErr.Error())
	}

	return nil
}

func (s *VolumeService) PruneVolumes(ctx context.Context) (*volumetypes.PruneReport, error) {
	return s.PruneVolumesWithOptions(ctx, false)
}

func (s *VolumeService) PruneVolumesWithOptions(ctx context.Context, all bool) (*volumetypes.PruneReport, error) {
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
		slog.WarnContext(ctx, "could not log volume prune action", "error", logErr.Error())
	}

	docker.InvalidateVolumeUsageCache()

	return &volumetypes.PruneReport{
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

// VolumeSizeData holds size information for a volume.
type VolumeSizeData struct {
	Size     int64
	RefCount int64
}

// GetVolumeSizes returns disk usage data for all volumes.
// This is a slow operation as it calls Docker's DiskUsage API.
func (s *VolumeService) GetVolumeSizes(ctx context.Context) (map[string]VolumeSizeData, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	usageVolumes, err := docker.GetVolumeUsageData(ctx, dockerClient)
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

func (s *VolumeService) buildVolumePaginationConfig() pagination.Config[volumetypes.Volume] {
	return pagination.Config[volumetypes.Volume]{
		SearchAccessors: []pagination.SearchAccessor[volumetypes.Volume]{
			func(v volumetypes.Volume) (string, error) { return v.Name, nil },
			func(v volumetypes.Volume) (string, error) { return v.Driver, nil },
			func(v volumetypes.Volume) (string, error) { return v.Mountpoint, nil },
			func(v volumetypes.Volume) (string, error) { return v.Scope, nil },
		},
		SortBindings:    s.buildVolumeSortBindings(),
		FilterAccessors: s.buildVolumeFilterAccessors(),
	}
}

func (s *VolumeService) buildVolumeSortBindings() []pagination.SortBinding[volumetypes.Volume] {
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
			Fn:  func(a, b volumetypes.Volume) int { return strings.Compare(a.CreatedAt, b.CreatedAt) },
		},
		{
			Key: "inUse",
			Fn: func(a, b volumetypes.Volume) int {
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

func (s *VolumeService) compareVolumeSizes(a, b volumetypes.Volume) int {
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

func (s *VolumeService) buildVolumeFilterAccessors() []pagination.FilterAccessor[volumetypes.Volume] {
	return []pagination.FilterAccessor[volumetypes.Volume]{
		{
			Key: "inUse",
			Fn: func(v volumetypes.Volume, filterValue string) bool {
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

func (s *VolumeService) calculateVolumeUsageCounts(items []volumetypes.Volume) volumetypes.UsageCounts {
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

func (s *VolumeService) buildPaginationResponse(result pagination.FilterResult[volumetypes.Volume], params pagination.QueryParams) pagination.Response {
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

func (s *VolumeService) ListVolumesPaginated(ctx context.Context, params pagination.QueryParams) ([]volumetypes.Volume, pagination.Response, volumetypes.UsageCounts, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, volumetypes.UsageCounts{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Run volume list and container list in parallel for better performance
	type volumeListResult struct {
		volumes []*volume.Volume
		err     error
	}
	type containerMapResult struct {
		containerMap map[string][]string
		err          error
	}

	volChan := make(chan volumeListResult, 1)
	containerChan := make(chan containerMapResult, 1)

	go func() {
		volListBody, err := dockerClient.VolumeList(ctx, volume.ListOptions{})
		volChan <- volumeListResult{volumes: volListBody.Volumes, err: err}
	}()

	go func() {
		containerMap, err := s.buildVolumeContainerMap(ctx, dockerClient)
		containerChan <- containerMapResult{containerMap: containerMap, err: err}
	}()

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

	// Fetch usage data if sorting by size is requested
	var usageVolumes []volume.Volume
	if params.Sort == "size" {
		if uv, err := docker.GetVolumeUsageData(ctx, dockerClient); err == nil {
			usageVolumes = uv
		} else {
			slog.WarnContext(ctx, "failed to get volume usage data for sorting", "error", err.Error())
		}
	}

	volumes := s.enrichVolumesWithUsageData(volResult.volumes, usageVolumes)

	items := make([]volumetypes.Volume, 0, len(volumes))
	for _, v := range volumes {
		volDto := volumetypes.NewSummary(v)
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
