package container

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
)

type resourceStatsServerInternal struct {
	mu          sync.Mutex
	containers  []container.Summary
	stats       map[string]container.StatsResponse
	statsStatus map[string]int
	statsCalls  map[string]int
	delay       time.Duration
	slowIDs     map[string]bool

	current atomic.Int32
	maxSeen atomic.Int32
}

func newResourceStatsServerInternal() *resourceStatsServerInternal {
	return &resourceStatsServerInternal{
		stats:       map[string]container.StatsResponse{},
		statsStatus: map[string]int{},
		statsCalls:  map[string]int{},
		slowIDs:     map[string]bool{},
	}
}

func (f *resourceStatsServerInternal) addContainer(id, name, state string, labels ...map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var l map[string]string
	if len(labels) > 0 {
		l = labels[0]
	}
	f.containers = append(f.containers, container.Summary{ID: id, Names: []string{"/" + name}, State: container.ContainerState(state), Image: "img:latest", Labels: l})
}

func (f *resourceStatsServerInternal) setStats(id string, cpuPercent float64, memoryUsage, memoryLimit uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// cpuPercent maps to cpuDelta/systemDelta*100 with a fixed systemDelta of 1000.
	f.stats[id] = container.StatsResponse{
		CPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: uint64(1000 + cpuPercent*10)},
			SystemUsage: 2000,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 1000},
			SystemUsage: 1000,
		},
		MemoryStats: container.MemoryStats{
			Usage: memoryUsage,
			Limit: memoryLimit,
		},
		Read: time.Now(),
	}
}

func (f *resourceStatsServerInternal) callsFor(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statsCalls[id]
}

func (f *resourceStatsServerInternal) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := dockerTestPathInternal(r.URL.Path)
		switch {
		case path == "/containers/json":
			f.mu.Lock()
			list := append([]container.Summary{}, f.containers...)
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(list)
		case strings.HasPrefix(path, "/containers/") && strings.HasSuffix(path, "/json"):
			id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/json")
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, summary := range f.containers {
				if summary.ID == id {
					inspect := container.InspectResponse{ID: summary.ID, Name: summary.Names[0], Image: summary.ImageID, Config: &container.Config{Image: summary.Image, Labels: summary.Labels}, State: &container.State{Status: summary.State}}
					if err := json.NewEncoder(w).Encode(inspect); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
					}
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		case strings.HasPrefix(path, "/containers/") && strings.HasSuffix(path, "/stats"):
			id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/stats")
			cur := f.current.Add(1)
			for {
				seen := f.maxSeen.Load()
				if cur <= seen || f.maxSeen.CompareAndSwap(seen, cur) {
					break
				}
			}
			defer f.current.Add(-1)

			f.mu.Lock()
			f.statsCalls[id]++
			stats, hasStats := f.stats[id]
			status, hasStatus := f.statsStatus[id]
			delay := f.delay
			slow := f.slowIDs[id]
			f.mu.Unlock()

			wait := delay
			if slow {
				wait = time.Second
			}
			if wait > 0 {
				select {
				case <-time.After(wait):
				case <-r.Context().Done():
					return
				}
			}
			if hasStatus {
				w.WriteHeader(status)
				return
			}
			if !hasStats {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(stats)
		default:
			http.NotFound(w, r)
		}
	})
}

func newResourceSortServiceInternal(t *testing.T, fixture *resourceStatsServerInternal) *ContainerService {
	t.Helper()
	server := httptest.NewServer(fixture.handler())
	t.Cleanup(server.Close)

	return NewContainerService(
		nil,
		docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server)),
		nil,
		nil,
		nil,
	)
}

func resourceSortParamsInternal(sort, order string, start, limit int) pagination.QueryParams {
	return pagination.QueryParams{
		SortParams: pagination.SortParams{Sort: sort, Order: pagination.SortOrder(order)},
		Params:     pagination.Params{Start: start, Limit: limit},
		Filters:    map[string]string{},
	}
}

func summaryIDsInternal(items []containertypes.Summary) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func listResourceSortedInternal(t *testing.T, svc *ContainerService, params pagination.QueryParams) ContainerListResult {
	t.Helper()
	result, err := svc.ListContainersPaginated(t.Context(), params, true, true, true, "")
	require.NoError(t, err)
	return result
}

func TestResourceSortRanksMemoryByBytesNotPercent(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	// high-percent: 800 of 1000 bytes (80%); high-bytes: 900 of 10000 (9%).
	fixture.addContainer("high-percent", "high-percent", "running")
	fixture.setStats("high-percent", 10, 800, 1000)
	fixture.addContainer("high-bytes", "high-bytes", "running")
	fixture.setStats("high-bytes", 10, 900, 10000)
	fixture.addContainer("opted-out", "opted-out", "running", map[string]string{"com.getarcaneapp.arcane.updater": "false"})
	fixture.setStats("opted-out", 10, 100, 10000)
	svc := newResourceSortServiceInternal(t, fixture)

	result := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20))
	require.Equal(t, []string{"high-bytes", "high-percent", "opted-out"}, summaryIDsInternal(result.Items))
	require.True(t, result.Items[0].AutoUpdateEnabled)
	require.False(t, result.Items[2].AutoUpdateEnabled)

	result = listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "asc", 0, 20))
	require.Equal(t, []string{"opted-out", "high-percent", "high-bytes"}, summaryIDsInternal(result.Items))

	flat, err := svc.ListContainersPaginated(t.Context(), resourceSortParamsInternal("name", "asc", 0, 20), true, true, true, "")
	require.NoError(t, err)
	require.Equal(t, []string{"high-bytes", "high-percent", "opted-out"}, summaryIDsInternal(flat.Items))
	require.True(t, flat.Items[0].AutoUpdateEnabled)
	require.False(t, flat.Items[2].AutoUpdateEnabled)

	details, err := svc.GetContainerDetails(t.Context(), "opted-out")
	require.NoError(t, err)
	require.False(t, details.AutoUpdateEnabled, "detail status must match the list status")
	details, err = svc.GetContainerDetails(t.Context(), "high-bytes")
	require.NoError(t, err)
	require.True(t, details.AutoUpdateEnabled)
}

func TestResourceSortOrdersAcrossPages(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	memoryByID := map[string]uint64{"c1": 100, "c2": 200, "c3": 300, "c4": 400, "c5": 500}
	for id, memory := range memoryByID {
		fixture.addContainer(id, id, "running")
		fixture.setStats(id, 0, memory, 10000)
	}
	svc := newResourceSortServiceInternal(t, fixture)

	tests := []struct {
		name  string
		order string
		start int
		want  []string
	}{
		{name: "desc page 1", order: "desc", start: 0, want: []string{"c5", "c4"}},
		{name: "desc page 2", order: "desc", start: 2, want: []string{"c3", "c2"}},
		{name: "desc page 3", order: "desc", start: 4, want: []string{"c1"}},
		{name: "asc page 1", order: "asc", start: 0, want: []string{"c1", "c2"}},
		{name: "asc page 2", order: "asc", start: 2, want: []string{"c3", "c4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, tt.order, tt.start, 2))
			require.Equal(t, tt.want, summaryIDsInternal(result.Items))
			require.Equal(t, int64(5), result.Pagination.TotalItems)
		})
	}
}

func TestResourceSortOrdersByCPUPercent(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("busy", "busy", "running")
	fixture.setStats("busy", 90, 100, 1000)
	fixture.addContainer("idle", "idle", "running")
	fixture.setStats("idle", 5, 100, 1000)
	svc := newResourceSortServiceInternal(t, fixture)

	result := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortCPUUsage, "desc", 0, 20))
	require.Equal(t, []string{"busy", "idle"}, summaryIDsInternal(result.Items))
	require.InDelta(t, 90, result.Items[0].ResourceSample.CPUPercent, 0.2)
	require.InDelta(t, 5, result.Items[1].ResourceSample.CPUPercent, 0.2)
}

func TestResourceSortAppliesSearchAndFiltersBeforeCollectingStats(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("web", "web", "running", map[string]string{"team": "backend"})
	fixture.setStats("web", 10, 100, 1000)
	fixture.addContainer("db", "db", "running")
	fixture.setStats("db", 20, 200, 1000)
	svc := newResourceSortServiceInternal(t, fixture)

	params := resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20)
	params.Search = "web"
	result := listResourceSortedInternal(t, svc, params)
	require.Equal(t, []string{"web"}, summaryIDsInternal(result.Items))
	require.Equal(t, 1, fixture.callsFor("web"))
	require.Equal(t, 0, fixture.callsFor("db"), "filtered-out containers must not trigger stats collection")

	params = resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20)
	params.Filters["label"] = "team=backend"
	result = listResourceSortedInternal(t, svc, params)
	require.Equal(t, []string{"web"}, summaryIDsInternal(result.Items))
	require.Equal(t, 0, fixture.callsFor("db"))
}

func TestResourceSortHandlesZeroUnavailableStoppedAndTies(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("zero", "zero", "running")
	fixture.setStats("zero", 0, 0, 1000)
	fixture.addContainer("tie-b", "alpha", "running")
	fixture.setStats("tie-b", 0, 100, 1000)
	fixture.addContainer("tie-a", "alpha", "running")
	fixture.setStats("tie-a", 0, 100, 1000)
	fixture.addContainer("broken", "broken", "running")
	fixture.statsStatus["broken"] = http.StatusInternalServerError
	fixture.addContainer("stopped", "stopped", "exited")
	svc := newResourceSortServiceInternal(t, fixture)

	asc := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "asc", 0, 20))
	require.Equal(t, []string{"zero", "tie-a", "tie-b", "broken", "stopped"}, summaryIDsInternal(asc.Items),
		"zeros first, unavailable and stopped last, ties by name then ID")
	require.NotNil(t, asc.Items[0].ResourceSample, "zero usage is a valid sample, not unavailable")
	require.Nil(t, asc.Items[3].ResourceSample)
	require.Nil(t, asc.Items[4].ResourceSample)

	desc := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20))
	require.Equal(t, []string{"tie-a", "tie-b", "zero", "broken", "stopped"}, summaryIDsInternal(desc.Items),
		"unavailable stays last in descending order too")

	require.Equal(t, 0, fixture.callsFor("stopped"), "stopped containers must not trigger stats collection")
}

func TestResourceSortCachesSamples(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("web", "web", "running")
	fixture.setStats("web", 10, 100, 1000)
	svc := newResourceSortServiceInternal(t, fixture)

	for range 2 {
		listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20))
	}
	require.Equal(t, 1, fixture.callsFor("web"), "second list within the TTL must reuse the cached sample")
}

func TestResourceSortCacheExpires(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("web", "web", "running")
	fixture.setStats("web", 10, 100, 1000)
	svc := newResourceSortServiceInternal(t, fixture)
	svc.resourceSampleCache = newResourceSampleCacheInternal(50 * time.Millisecond)

	listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20))
	time.Sleep(100 * time.Millisecond)
	listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20))
	require.Equal(t, 2, fixture.callsFor("web"), "expired entries must be refetched")
}

func TestResourceSortCoalescesConcurrentFetches(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("web", "web", "running")
	fixture.setStats("web", 10, 100, 1000)
	fixture.delay = 100 * time.Millisecond
	svc := newResourceSortServiceInternal(t, fixture)

	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.fetchResourceSampleInternal(t.Context(), "web")
			errs[i] = err
		}()
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, fixture.callsFor("web"), "concurrent fetches for the same container must coalesce")
}

func TestResourceSortBoundsCollectionConcurrency(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.delay = 50 * time.Millisecond
	for i := range 32 {
		id := fmt.Sprintf("c%02d", i)
		fixture.addContainer(id, id, "running")
		fixture.setStats(id, 0, uint64(i), 10000)
	}
	svc := newResourceSortServiceInternal(t, fixture)

	result := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 40))
	require.Len(t, result.Items, 32)
	require.LessOrEqual(t, fixture.maxSeen.Load(), int32(containerResourceCollectConcurrency))
}

func TestResourceSortBatchTimeoutFailsRequest(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("slow", "slow", "running")
	fixture.setStats("slow", 10, 100, 1000)
	fixture.delay = time.Second
	svc := newResourceSortServiceInternal(t, fixture)
	svc.resourceBatchTimeout = 50 * time.Millisecond

	_, err := svc.ListContainersPaginated(t.Context(), resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20), true, true, true, "")
	require.Error(t, err, "batch timeout must fail the refresh instead of presenting a partial sort")
}

func TestResourceSortPerContainerTimeoutMarksUnavailable(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("fast", "fast", "running")
	fixture.setStats("fast", 10, 100, 1000)
	fixture.addContainer("slow", "slow", "running")
	fixture.setStats("slow", 10, 200, 1000)
	fixture.slowIDs["slow"] = true
	svc := newResourceSortServiceInternal(t, fixture)
	svc.resourceSampleTimeout = 50 * time.Millisecond

	result := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20))
	require.Equal(t, []string{"fast", "slow"}, summaryIDsInternal(result.Items))
	require.NotNil(t, result.Items[0].ResourceSample)
	require.Nil(t, result.Items[1].ResourceSample, "per-container timeout leaves the sample unavailable")
}

func TestResourceSortCancelledContextFailsRequest(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("web", "web", "running")
	fixture.setStats("web", 10, 100, 1000)
	fixture.delay = time.Second
	svc := newResourceSortServiceInternal(t, fixture)

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := svc.ListContainersPaginated(ctx, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20), true, true, true, "")
	require.Error(t, err)
}

func TestResourceSortPermissionCheck(t *testing.T) {
	listOnly := authz.NewPermissionSet()
	listOnly.AddEnv("env-1", authz.PermContainersList)
	full := authz.NewPermissionSet()
	full.AddEnv("env-1", authz.PermContainersList, authz.PermContainersRead)

	require.True(t, containerResourceSortPermissionDeniedInternal(listOnly, "env-1", containertypes.SortCPUUsage))
	require.True(t, containerResourceSortPermissionDeniedInternal(listOnly, "env-1", containertypes.SortMemoryUsage))
	require.False(t, containerResourceSortPermissionDeniedInternal(full, "env-1", containertypes.SortMemoryUsage))
	require.False(t, containerResourceSortPermissionDeniedInternal(listOnly, "env-1", "name"),
		"non-resource sorts keep the endpoint's containers:list requirement")
	require.False(t, containerResourceSortPermissionDeniedInternal(authz.SudoPermissionSet(), "env-1", containertypes.SortCPUUsage))
}

func TestResourceSortComparatorKeepsUnavailableLast(t *testing.T) {
	asc := containerResourceSampleSortInternal(containertypes.SortMemoryUsage, false)
	desc := containerResourceSampleSortInternal(containertypes.SortMemoryUsage, true)
	sampled := containertypes.Summary{ID: "b", Names: []string{"b"}, ResourceSample: &containertypes.ResourceSample{MemoryUsageBytes: 1}}
	unsampled := containertypes.Summary{ID: "a", Names: []string{"a"}}

	require.Equal(t, 1, asc(unsampled, sampled))
	require.Equal(t, -1, asc(sampled, unsampled))
	require.Equal(t, 1, desc(unsampled, sampled))
	require.Equal(t, -1, desc(sampled, unsampled))

	zero := containertypes.Summary{ID: "z", Names: []string{"z"}, ResourceSample: &containertypes.ResourceSample{MemoryUsageBytes: 0}}
	require.Equal(t, -1, asc(zero, unsampled), "zero is a valid value, not unavailable")
	require.Equal(t, -1, asc(zero, sampled))
}
