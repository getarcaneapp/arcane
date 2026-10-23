// Package stats owns container resource sampling, resource metrics, resource sorting and the
// live stats stream.
package stats

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/samber/hot"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/streams/stats"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
)

const (
	ContainerResourceSampleTTL          = 5 * time.Second
	ContainerResourceSampleCacheSize    = 4096
	ContainerResourceCollectConcurrency = 16
	ContainerResourceCollectTimeout     = 3 * time.Second
	ContainerMetricsCollectTimeout      = 20 * time.Second
)

// Service collects container stats through the parent's Docker client.
type Service struct {
	getClient     func(context.Context) (*client.Client, error)
	dockerHost    func() string
	history       stats.Store
	cache         *hot.HotCache[string, cachedStats]
	inspectCache  *hot.HotCache[string, container.InspectResponse]
	flight        singleflight.Group
	sampleTimeout time.Duration
	batchTimeout  time.Duration
}

// cachedStats records when Arcane fetched a sample, so freshness checks don't depend on the daemon's clock.
type cachedStats struct {
	stats     container.StatsResponse
	fetchedAt time.Time
}

type resourceSample struct {
	stats   container.StatsResponse
	inspect *container.InspectResponse
}

type resourceInstruments struct {
	cpuTime                  metric.Float64ObservableCounter
	cpuUtilization, uptime   metric.Float64ObservableGauge
	memoryUsage, memoryLimit metric.Int64ObservableGauge
	pids                     metric.Int64ObservableGauge
	networkIO, diskIO        metric.Int64ObservableCounter
	restarts                 metric.Int64ObservableCounter
}

// New builds a stats service. Non-positive timeouts fall back to defaults.
func New(getClient func(context.Context) (*client.Client, error), dockerHost func() string, cacheTTL, sampleTimeout, batchTimeout time.Duration) *Service {
	return &Service{
		getClient:  getClient,
		dockerHost: dockerHost,
		cache: hot.NewHotCache[string, cachedStats](hot.LRU, ContainerResourceSampleCacheSize).
			WithTTL(cacheTTL).
			WithJanitor().
			Build(),
		// Restart count and start time only change on restart, so metrics reuse inspects for a minute.
		inspectCache: hot.NewHotCache[string, container.InspectResponse](hot.LRU, ContainerResourceSampleCacheSize).
			WithTTL(time.Minute).
			WithJanitor().
			Build(),
		sampleTimeout: kit.Ternary(sampleTimeout > 0, sampleTimeout, ContainerResourceCollectTimeout),
		batchTimeout:  kit.Ternary(batchTimeout > 0, batchTimeout, timeouts.DefaultDockerAPI),
	}
}

// Stream decodes the Docker stats stream and forwards payloads with history.
func (s *Service) Stream(ctx context.Context, containerID string, statsChan chan<- any) error {
	dockerClient, err := s.getClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	statsResponse, err := dockerClient.ContainerStats(ctx, containerID, client.ContainerStatsOptions{Stream: true})
	if err != nil {
		return fmt.Errorf("failed to start stats stream: %w", err)
	}
	defer func() { _ = statsResponse.Body.Close() }()

	decoder := jsontext.NewDecoder(statsResponse.Body)
	historySent := false

	for {
		if cancellationErr := ctx.Err(); cancellationErr != nil {
			return cancellationErr
		}

		var statsData container.StatsResponse
		if unmarshalDecodeErr := json.UnmarshalDecode(decoder, &statsData); unmarshalDecodeErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(unmarshalDecodeErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("failed to decode stats: %w", unmarshalDecodeErr)
		}

		currentSample := stats.BuildSample(statsData)
		payload := stats.StatsStreamPayload{
			StatsResponse:        statsData,
			CurrentHistorySample: currentSample,
			StatsHistory:         s.history.Record(containerID, currentSample, !historySent, kit.Ternary(statsData.Read.IsZero(), time.Now(), statsData.Read)),
		}
		if historySent && statsData.PreCPUStats.SystemUsage != 0 {
			s.cache.Set(s.dockerHost()+"\x00"+containerID, cachedStats{stats: statsData, fetchedAt: time.Now()})
		}
		historySent = true

		select {
		case statsChan <- payload:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Collect fetches one sample per running container, reusing cached samples younger than maxAge.
// Individual failures leave the container unsampled; a batch timeout or cancellation fails so partial
// lists never sort as global.
func (s *Service) Collect(ctx context.Context, items []types.ContainerSummary, maxAge time.Duration) (map[string]*types.ContainerResourceSample, error) {
	ids := make([]string, 0, len(items))
	for i := range items {
		if items[i].State == string(container.StateRunning) {
			ids = append(ids, items[i].ID)
		}
	}
	raw, err := collect(ctx, s.batchTimeout, ids, func(ctx context.Context, id string) (container.StatsResponse, error) {
		return s.fetch(ctx, id, maxAge)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to collect container resource samples: %w", err)
	}
	samples := make(map[string]*types.ContainerResourceSample, len(raw))
	for id, statsData := range raw {
		built := stats.BuildSample(statsData)
		samples[id] = &types.ContainerResourceSample{
			CPUPercent:       float64(built.CPUTenths) / 10,
			MemoryUsageBytes: built.MemoryUsageBytes,
			MemoryLimitBytes: statsData.MemoryStats.Limit,
			SampleTime:       statsData.Read,
		}
	}
	return samples, nil
}

// collect runs fetch for each ID with bounded concurrency under timeout. Individual failures are
// skipped; on batch timeout or cancellation it returns the partial result with the error.
func collect[T any](ctx context.Context, timeout time.Duration, ids []string, fetch func(context.Context, string) (T, error)) (map[string]T, error) {
	batchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	g, groupCtx := errgroup.WithContext(batchCtx)
	g.SetLimit(ContainerResourceCollectConcurrency)
	results := make(map[string]T, len(ids))
	var mu sync.Mutex
	for _, id := range ids {
		g.Go(func() error {
			result, err := fetch(groupCtx, id)
			if err != nil {
				if groupCtx.Err() != nil {
					return groupCtx.Err()
				}
				slog.WarnContext(groupCtx, "Failed to collect container resource sample", "containerId", id, "error", err)
				return nil
			}
			mu.Lock()
			results[id] = result
			mu.Unlock()
			return nil
		})
	}
	return results, g.Wait()
}

// fetch returns a cached stats sample younger than maxAge and coalesces concurrent fetches per daemon
// and container. IncludePreviousSample gives the CPU delta a valid previous sample.
func (s *Service) fetch(ctx context.Context, containerID string, maxAge time.Duration) (container.StatsResponse, error) {
	key := s.dockerHost() + "\x00" + containerID
	if cached, ok, _ := s.cache.Get(key); ok && time.Since(cached.fetchedAt) < maxAge {
		return cached.stats, nil
	}

	ch := s.flight.DoChan(key, func() (any, error) {
		flightCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.sampleTimeout)
		defer cancel()

		dockerClient, err := s.getClient(flightCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Docker: %w", err)
		}
		statsResponse, err := dockerClient.ContainerStats(flightCtx, containerID, client.ContainerStatsOptions{Stream: false, IncludePreviousSample: true})
		if err != nil {
			return nil, fmt.Errorf("failed to fetch container stats: %w", err)
		}
		defer func() { _ = statsResponse.Body.Close() }()

		var statsData container.StatsResponse
		if decodeErr := json.UnmarshalDecode(jsontext.NewDecoder(statsResponse.Body), &statsData); decodeErr != nil {
			return nil, fmt.Errorf("failed to decode container stats: %w", decodeErr)
		}
		if statsData.Read.IsZero() {
			statsData.Read = time.Now() // keep: StatsResponse is filled by json.UnmarshalDecode and has ~15 nested fields
		}
		s.cache.Set(key, cachedStats{stats: statsData, fetchedAt: time.Now()})
		return statsData, nil
	})

	fetchCtx, cancel := context.WithTimeout(ctx, s.sampleTimeout)
	defer cancel()
	select {
	case <-fetchCtx.Done():
		return container.StatsResponse{}, fetchCtx.Err()
	case res := <-ch:
		if res.Err != nil {
			return container.StatsResponse{}, res.Err
		}
		statsData, ok := res.Val.(container.StatsResponse)
		if !ok {
			return container.StatsResponse{}, errors.New("resource sample flight returned unexpected type")
		}
		return statsData, nil
	}
}

// ObserveResources reports arcane.container.* metrics for running containers from list, sharing the
// sample cache and flights with Collect so overlapping collections reuse Docker calls.
func (s *Service) ObserveResources(meter metric.Meter, list func(context.Context) ([]container.Summary, error)) error {
	cpuTime, cpuTimeErr := meter.Float64ObservableCounter("arcane.container.cpu.time",
		metric.WithDescription("Cumulative CPU time consumed by a running container"), metric.WithUnit("s"))
	cpuUtilization, cpuUtilizationErr := meter.Float64ObservableGauge("arcane.container.cpu.utilization",
		metric.WithDescription("Container CPU usage as a fraction of total host CPU capacity"), metric.WithUnit("1"))
	memoryUsage, memoryUsageErr := meter.Int64ObservableGauge("arcane.container.memory.usage",
		metric.WithDescription("Container memory usage excluding inactive page cache"), metric.WithUnit("By"))
	memoryLimit, memoryLimitErr := meter.Int64ObservableGauge("arcane.container.memory.limit",
		metric.WithDescription("Container memory limit, or host memory when unlimited"), metric.WithUnit("By"))
	pids, pidsErr := meter.Int64ObservableGauge("arcane.container.pids",
		metric.WithDescription("Processes and threads running in a container"), metric.WithUnit("{process}"))
	networkIO, networkIOErr := meter.Int64ObservableCounter("arcane.container.network.io",
		metric.WithDescription("Bytes received and transmitted across all container network interfaces"), metric.WithUnit("By"))
	diskIO, diskIOErr := meter.Int64ObservableCounter("arcane.container.disk.io",
		metric.WithDescription("Bytes read from and written to block devices by a container"), metric.WithUnit("By"))
	restarts, restartsErr := meter.Int64ObservableCounter("arcane.container.restarts",
		metric.WithDescription("Times Docker restarted the container under its restart policy"), metric.WithUnit("{restart}"))
	uptime, uptimeErr := meter.Float64ObservableGauge("arcane.container.uptime",
		metric.WithDescription("Time since the container last started"), metric.WithUnit("s"))
	if err := errors.Join(cpuTimeErr, cpuUtilizationErr, memoryUsageErr, memoryLimitErr, pidsErr, networkIOErr, diskIOErr, restartsErr, uptimeErr); err != nil {
		return err
	}
	r := resourceInstruments{
		cpuTime:        cpuTime,
		cpuUtilization: cpuUtilization,
		uptime:         uptime,
		memoryUsage:    memoryUsage,
		memoryLimit:    memoryLimit,
		pids:           pids,
		networkIO:      networkIO,
		diskIO:         diskIO,
		restarts:       restarts,
	}

	_, err := meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		containers, err := list(ctx)
		if err != nil {
			slog.DebugContext(ctx, "Skipping container resource metrics", "error", err)
			return nil
		}
		running := make(map[string]container.Summary, len(containers))
		for _, c := range containers {
			if c.State == container.StateRunning {
				running[c.ID] = c
			}
		}
		samples, err := collect(ctx, ContainerMetricsCollectTimeout, slices.Collect(maps.Keys(running)), func(ctx context.Context, id string) (resourceSample, error) {
			statsData, fetchErr := s.fetch(ctx, id, ContainerResourceSampleTTL)
			if fetchErr != nil {
				return resourceSample{}, fetchErr
			}
			return resourceSample{
				stats: statsData,
				inspect: func() *container.InspectResponse {
					if cached, ok, _ := s.inspectCache.Get(id); ok {
						return &cached
					}
					dockerClient, clientErr := s.getClient(ctx)
					if clientErr != nil {
						return nil
					}
					inspect, inspectErr := dockerClient.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
					if inspectErr != nil {
						return nil
					}
					s.inspectCache.Set(id, inspect.Container)
					return &inspect.Container
				}(),
			}, nil
		})
		if err != nil {
			slog.DebugContext(ctx, "Container resource metrics are partial", "error", err)
		}
		for id, sample := range samples {
			r.observe(o, running[id], sample)
		}
		return nil
	}, r.cpuTime, r.cpuUtilization, r.memoryUsage, r.memoryLimit, r.pids, r.networkIO, r.diskIO, r.restarts, r.uptime)
	return err
}

// observe records one container's sample, labelled by name, image and Compose project/service.
func (r *resourceInstruments) observe(o metric.Observer, c container.Summary, sample resourceSample) {
	attrs := []attribute.KeyValue{
		attribute.String("container.name", docker.ContainerNameFromNames(c.Names)),
		attribute.String("container.image.name", c.Image),
	}
	if project := docker.ComposeProjectLabel(c.Labels); project != "" {
		attrs = append(attrs, attribute.String("compose.project", project))
	}
	if service := docker.ComposeServiceLabel(c.Labels); service != "" {
		attrs = append(attrs, attribute.String("compose.service", service))
	}
	labels := metric.WithAttributes(attrs...)
	with := func(kv attribute.KeyValue) metric.MeasurementOption {
		return metric.WithAttributes(append(attrs[:len(attrs):len(attrs)], kv)...)
	}

	statsData := sample.stats
	built := stats.BuildSample(statsData)
	o.ObserveFloat64(r.cpuTime, float64(statsData.CPUStats.CPUUsage.TotalUsage)/float64(time.Second), labels)
	o.ObserveFloat64(r.cpuUtilization, float64(built.CPUTenths)/1000, labels)
	o.ObserveInt64(r.memoryUsage, int64(built.MemoryUsageBytes), labels)
	o.ObserveInt64(r.memoryLimit, int64(statsData.MemoryStats.Limit), labels)
	o.ObserveInt64(r.pids, int64(statsData.PidsStats.Current), labels)

	var rx, tx, read, write uint64
	for _, network := range statsData.Networks {
		rx += network.RxBytes
		tx += network.TxBytes
	}
	for _, entry := range statsData.BlkioStats.IoServiceBytesRecursive {
		switch strings.ToLower(entry.Op) {
		case "read":
			read += entry.Value
		case "write":
			write += entry.Value
		}
	}
	o.ObserveInt64(r.networkIO, int64(rx), with(attribute.String("network.io.direction", "receive")))
	o.ObserveInt64(r.networkIO, int64(tx), with(attribute.String("network.io.direction", "transmit")))
	o.ObserveInt64(r.diskIO, int64(read), with(attribute.String("disk.io.direction", "read")))
	o.ObserveInt64(r.diskIO, int64(write), with(attribute.String("disk.io.direction", "write")))

	if sample.inspect == nil {
		return
	}
	o.ObserveInt64(r.restarts, int64(sample.inspect.RestartCount), labels)
	if startedAt, err := time.Parse(time.RFC3339Nano, kit.FromPtr(sample.inspect.State).StartedAt); err == nil {
		o.ObserveFloat64(r.uptime, time.Since(startedAt).Seconds(), labels)
	}
}

// ContainerResourceSampleSort orders summaries by sample value with unsampled containers last in both
// directions, breaking ties by name then ID. Valid zero values sort as zero, not as unavailable.
func ContainerResourceSampleSort(sort string, descending bool) pagination.SortOption[types.ContainerSummary] {
	value := kit.Ternary(
		sort == types.ContainerSortCPUUsage,
		func(sample *types.ContainerResourceSample) float64 { return sample.CPUPercent },
		func(sample *types.ContainerResourceSample) float64 { return float64(sample.MemoryUsageBytes) },
	)

	tieBreak := func(a, b types.ContainerSummary) int {
		if c := CompareContainerNamesForSort(a, b); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	}

	return func(a, b types.ContainerSummary) int {
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
		return kit.Ternary((valueA < valueB) != descending, -1, 1)
	}
}

func CompareContainerNamesForSort(a, b types.ContainerSummary) int {
	firstName := func(names []string) string {
		if len(names) == 0 {
			return ""
		}
		return names[0]
	}
	return strings.Compare(firstName(a.Names), firstName(b.Names))
}

// ContainerResourceSortPermissionDenied enforces the extra containers:read
// requirement on resource-sorted list requests.
func ContainerResourceSortPermissionDenied(ps *authz.PermissionSet, envID, sort string) bool {
	if !types.IsContainerResourceSort(sort) {
		return false
	}
	return !ps.Allows(authz.PermContainersRead, envID)
}
