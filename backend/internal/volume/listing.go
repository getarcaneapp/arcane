package volume

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"emperror.dev/errors"

	docker "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
)

func (s *VolumeService) GetVolumeUsage(ctx context.Context, name string) (bool, []string, error) {
	slog.DebugContext(ctx, "volume service: get volume usage", "volume", name)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return false, nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	vol, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err != nil {
		return false, nil, errors.WrapIf(err, "volume not found")
	}

	containerIDs, err := docker.GetContainersUsingVolume(ctx, dockerClient, vol.Volume.Name)
	if err != nil {
		return false, nil, errors.WrapIf(err, "failed to get containers using volume")
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
	settings := s.settingsService.GetSettingsConfig()
	apiCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI))
	defer cancel()

	dockerClient, err := s.dockerService.GetClient(apiCtx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	usageVolumes, err := docker.GetVolumeUsageData(apiCtx, dockerClient)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get volume usage data")
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

func enrichVolumesWithUsageDataInternal(volumes []volume.Volume, usageVolumes []volume.Volume) []volume.Volume {
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

	result := make([]volume.Volume, 0, len(volumes))
	for _, v := range volumes {
		if usageData, exists := usageByName[v.Name]; exists {
			v.UsageData = usageData
		}

		result = append(result, v)
	}
	return result
}

func buildVolumeContainerMapInternal(ctx context.Context, dockerClient *client.Client) (map[string][]string, error) {
	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, errors.WrapIf(err, "failed to list containers")
	}

	volumeContainerMap := make(map[string][]string)
	for _, c := range containers.Items {
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
				if a.InUse {
					return -1
				}
				return 1
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
	if aSize < bSize {
		return -1
	}
	return 1
}

func (s *VolumeService) compareVolumeCreatedInternal(a, b volumetypes.Volume) int {
	aTime, aOk := parseVolumeCreatedAtInternal(a.CreatedAt).Get()
	bTime, bOk := parseVolumeCreatedAtInternal(b.CreatedAt).Get()
	if aOk && bOk {
		if aTime.Before(bTime) {
			return -1
		}
		if aTime.After(bTime) {
			return 1
		}
		return 0
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
func (s *VolumeService) CountUsageFromSnapshot(volumes []volume.Volume, containers []container.Summary) volumetypes.UsageCounts {
	inUse := make(map[string]struct{})
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name != "" {
				inUse[m.Name] = struct{}{}
			}
		}
	}

	counts := volumetypes.UsageCounts{}
	for _, v := range volumes {
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

	return libarcane.IsInternalContainer(v.Labels)
}

func (s *VolumeService) ListVolumesPaginated(ctx context.Context, params pagination.QueryParams, includeInternal bool) ([]volumetypes.Volume, pagination.Response, volumetypes.UsageCounts, error) {
	startedAt := time.Now()
	slog.DebugContext(ctx, "volume service: list volumes paginated", "search", params.Search, "sort", params.Sort, "order", params.Order, "start", params.Start, "limit", params.Limit, "include_internal", includeInternal)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, pagination.Response{}, volumetypes.UsageCounts{}, errors.WrapIf(err, "failed to connect to Docker")
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

	settings := s.settingsService.GetSettingsConfig()
	apiCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI))
	defer cancel()

	go func(ctx context.Context) {
		volListBody, err := dockerClient.VolumeList(ctx, client.VolumeListOptions{})
		volChan <- volumeListResult{volumes: volListBody.Items, err: err}
	}(apiCtx)

	go func(ctx context.Context) {
		containerMap, err := buildVolumeContainerMapInternal(ctx, dockerClient)
		containerChan <- containerMapResult{containerMap: containerMap, err: err}
	}(apiCtx)

	// Wait for both results
	volResult := <-volChan
	if volResult.err != nil {
		return nil, pagination.Response{}, volumetypes.UsageCounts{}, errors.WrapIf(volResult.err, "failed to list Docker volumes")
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

	volumes := enrichVolumesWithUsageDataInternal(volResult.volumes, usageVolumes)

	items := make([]volumetypes.Volume, 0, len(volumes))
	for _, v := range volumes {
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

	config := s.buildVolumePaginationConfigInternal()
	result := config.SearchOrderAndPaginate(items, effectiveParams)
	counts := calculateVolumeUsageCountsInternal(items)
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, effectiveParams)
	slog.DebugContext(ctx, "volume service: listed volumes",
		"docker_host", dockerClient.DaemonHost(),
		"requested_sort", params.Sort,
		"requested_order", params.Order,
		"effective_sort", effectiveParams.Sort,
		"effective_order", effectiveParams.Order,
		"usage_cache_snapshot", usageCacheSnapshot,
		"docker_volumes", len(volResult.volumes),
		"usage_volumes", len(usageVolumes),
		"included_volumes", len(items),
		"matched_volumes", result.TotalCount,
		"returned_volumes", len(result.Items),
		"container_volume_count", len(volumeContainerMap),
		"filter_count", len(params.Filters),
		"current_page", paginationResp.CurrentPage,
		"total_pages", paginationResp.TotalPages,
		"duration", time.Since(startedAt),
	)

	return result.Items, paginationResp, counts, nil
}
