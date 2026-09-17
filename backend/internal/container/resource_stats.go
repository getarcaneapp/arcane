package container

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"log/slog"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/samber/hot"
	containerstats "go.getarcane.app/streams/stats"
	"golang.org/x/sync/errgroup"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
)

const (
	containerResourceSampleTTL          = 5 * time.Second
	containerResourceSampleCacheSize    = 4096
	containerResourceCollectConcurrency = 16
	containerResourceCollectTimeout     = 3 * time.Second
)

func newResourceSampleCacheInternal(ttl time.Duration) *hot.HotCache[string, containertypes.ResourceSample] {
	return hot.NewHotCache[string, containertypes.ResourceSample](hot.LRU, containerResourceSampleCacheSize).
		WithTTL(ttl).
		WithJanitor().
		Build()
}

// resourceSampleCacheKeyInternal isolates cache and coalescing entries by
// Docker daemon and container ID.
func (s *ContainerService) resourceSampleCacheKeyInternal(containerID string) string {
	return s.dockerService.DockerHost() + "\x00" + containerID
}

// containerResourceSortPermissionDeniedInternal enforces the extra
// containers:read requirement on resource-sorted list requests.
func containerResourceSortPermissionDeniedInternal(ps *authz.PermissionSet, envID, sort string) bool {
	if !containertypes.IsResourceSort(sort) {
		return false
	}
	return !ps.Allows(authz.PermContainersRead, envID)
}

// listContainersByResourceInternal applies search and filters, collects stats
// for the matching containers, then sorts and paginates.
func (s *ContainerService) listContainersByResourceInternal(
	ctx context.Context,
	config pagination.Config[containertypes.Summary],
	items []containertypes.Summary,
	counts containertypes.StatusCounts,
	params pagination.QueryParams,
) (ContainerListResult, error) {
	filtered := make([]containertypes.Summary, 0, len(items))
	for _, item := range items {
		if config.MatchesSearchAndFilters(item, params) {
			filtered = append(filtered, item)
		}
	}

	samples, err := s.collectResourceSamplesInternal(ctx, filtered)
	if err != nil {
		return ContainerListResult{}, err
	}
	for i := range filtered {
		filtered[i].ResourceSample = samples[filtered[i].ID]
	}

	page := config.OrderAndPaginate(filtered, params)
	s.ApplySummaryIcons(ctx, page, nil)

	return ContainerListResult{
		Items:      page,
		Pagination: pagination.BuildResponse(int64(len(filtered)), int64(len(items)), params),
		Counts:     counts,
	}, nil
}

// collectResourceSamplesInternal fetches one sample per running container.
// Individual failures leave the container unsampled; a batch timeout or
// cancellation fails the request so partial lists never sort as global.
func (s *ContainerService) collectResourceSamplesInternal(ctx context.Context, items []containertypes.Summary) (map[string]*containertypes.ResourceSample, error) {
	batchTimeout := s.resourceBatchTimeout
	if batchTimeout <= 0 {
		batchTimeout = timeouts.DefaultDockerAPI
	}
	batchCtx, cancel := context.WithTimeout(ctx, batchTimeout)
	defer cancel()

	g, groupCtx := errgroup.WithContext(batchCtx)
	g.SetLimit(containerResourceCollectConcurrency)

	samples := make(map[string]*containertypes.ResourceSample, len(items))
	var mu sync.Mutex

	for i := range items {
		if items[i].State != string(container.StateRunning) {
			continue
		}
		id := items[i].ID
		g.Go(func() error {
			sample, err := s.fetchResourceSampleInternal(groupCtx, id)
			if err != nil {
				if groupCtx.Err() != nil {
					return groupCtx.Err()
				}
				slog.WarnContext(groupCtx, "Failed to collect container resource sample", "container_id", id, "error", err)
				return nil
			}
			mu.Lock()
			samples[id] = sample
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, errors.WrapIf(err, "failed to collect container resource samples")
	}
	return samples, nil
}

// fetchResourceSampleInternal returns a cached sample when fresh and coalesces
// concurrent fetches for the same container.
func (s *ContainerService) fetchResourceSampleInternal(ctx context.Context, containerID string) (*containertypes.ResourceSample, error) {
	key := s.resourceSampleCacheKeyInternal(containerID)
	if s.resourceSampleCache != nil {
		if sample, ok, _ := s.resourceSampleCache.Get(key); ok {
			return &sample, nil
		}
	}

	sampleTimeout := s.resourceSampleTimeout
	if sampleTimeout <= 0 {
		sampleTimeout = containerResourceCollectTimeout
	}
	ch := s.resourceSampleFlight.DoChan(key, func() (any, error) {
		flightCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sampleTimeout)
		defer cancel()
		return s.collectResourceSampleInternal(flightCtx, containerID)
	})

	fetchCtx, cancel := context.WithTimeout(ctx, sampleTimeout)
	defer cancel()

	select {
	case <-fetchCtx.Done():
		return nil, fetchCtx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		sample, ok := res.Val.(*containertypes.ResourceSample)
		if !ok {
			return nil, errors.New("resource sample flight returned unexpected type")
		}
		return sample, nil
	}
}

// collectResourceSampleInternal performs the non-streaming Docker stats call.
// IncludePreviousSample gives the CPU delta a valid previous sample.
func (s *ContainerService) collectResourceSampleInternal(ctx context.Context, containerID string) (*containertypes.ResourceSample, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	stats, err := dockerClient.ContainerStats(ctx, containerID, client.ContainerStatsOptions{Stream: false, IncludePreviousSample: true})
	if err != nil {
		return nil, errors.WrapIf(err, "failed to fetch container stats")
	}
	defer func() { _ = stats.Body.Close() }()

	var statsData container.StatsResponse
	if err := json.UnmarshalDecode(jsontext.NewDecoder(stats.Body), &statsData); err != nil {
		return nil, errors.WrapIf(err, "failed to decode container stats")
	}

	built := containerstats.BuildSample(statsData)
	sample := &containertypes.ResourceSample{
		CPUPercent:       float64(built.CPUTenths) / 10,
		MemoryUsageBytes: built.MemoryUsageBytes,
		MemoryLimitBytes: statsData.MemoryStats.Limit,
		SampleTime:       statsData.Read,
	}
	if sample.SampleTime.IsZero() {
		sample.SampleTime = time.Now()
	}

	if s.resourceSampleCache != nil {
		s.resourceSampleCache.Set(s.resourceSampleCacheKeyInternal(containerID), *sample)
	}
	return sample, nil
}

// containerResourceSampleSortInternal orders summaries by their collected
// sample value, keeping unsampled containers last in both directions and
// breaking ties by name, then ID. Valid zero values sort as zero, not as
// unavailable.
func containerResourceSampleSortInternal(sort string, descending bool) pagination.SortOption[containertypes.Summary] {
	var value func(*containertypes.ResourceSample) float64
	if sort == containertypes.SortCPUUsage {
		value = func(sample *containertypes.ResourceSample) float64 { return sample.CPUPercent }
	} else {
		value = func(sample *containertypes.ResourceSample) float64 { return float64(sample.MemoryUsageBytes) }
	}

	tieBreak := func(a, b containertypes.Summary) int {
		if c := compareContainerNamesForSortInternal(a, b); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	}

	return func(a, b containertypes.Summary) int {
		if a.ResourceSample == nil && b.ResourceSample == nil {
			return tieBreak(a, b)
		}
		if a.ResourceSample == nil {
			return 1
		}
		if b.ResourceSample == nil {
			return -1
		}

		valueA, valueB := value(a.ResourceSample), value(b.ResourceSample)
		if valueA == valueB {
			return tieBreak(a, b)
		}
		less := valueA < valueB
		if descending {
			less = !less
		}
		if less {
			return -1
		}
		return 1
	}
}
