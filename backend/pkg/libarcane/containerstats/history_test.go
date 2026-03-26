package containerstats

import (
	"testing"
	"time"

	containertypes "github.com/getarcaneapp/arcane/types/container"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
)

func TestBuildSample(t *testing.T) {
	stats := container.StatsResponse{
		CPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 250},
			SystemUsage: 500,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 50},
			SystemUsage: 100,
		},
		MemoryStats: container.MemoryStats{
			Usage: 700,
			Limit: 1000,
			Stats: map[string]uint64{"inactive_file": 100},
		},
	}

	sample := BuildSample(stats)

	require.Equal(t, uint16(500), sample.CPUTenths)
	require.Equal(t, uint16(600), sample.MemoryTenths)
	require.Equal(t, uint64(600), sample.MemoryUsageBytes)
}

func TestStoreRecordCapsAndPreservesOrder(t *testing.T) {
	var store Store
	baseTime := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)

	for i := 0; i < HistoryCapacity+5; i++ {
		store.Record(
			"container-1",
			containertypes.StatsHistorySample{
				CPUTenths:        uint16(i),
				MemoryTenths:     uint16(i + 100),
				MemoryUsageBytes: uint64(i + 1000),
			},
			false,
			baseTime.Add(time.Duration(i)*time.Second),
		)
	}

	snapshot := snapshotForTest(&store, "container-1", baseTime.Add(5*time.Minute))
	require.Len(t, snapshot, HistoryCapacity)

	for i, sample := range snapshot {
		expected := i + 5
		require.Equal(t, uint16(expected), sample.CPUTenths)
		require.Equal(t, uint16(expected+100), sample.MemoryTenths)
		require.Equal(t, uint64(expected+1000), sample.MemoryUsageBytes)
	}
}

func TestStoreRecordPrunesExpiredContainers(t *testing.T) {
	var store Store
	baseTime := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)

	store.Record("stale-container", containertypes.StatsHistorySample{CPUTenths: 111, MemoryTenths: 222, MemoryUsageBytes: 333}, false, baseTime)
	store.Record(
		"fresh-container",
		containertypes.StatsHistorySample{CPUTenths: 333, MemoryTenths: 444, MemoryUsageBytes: 555},
		false,
		baseTime.Add(historyTTL-historyPruneInterval),
	)

	staleSnapshot := snapshotForTest(&store, "stale-container", baseTime.Add(historyTTL+time.Minute))
	freshSnapshot := snapshotForTest(
		&store,
		"fresh-container",
		baseTime.Add(historyTTL-historyPruneInterval+time.Minute),
	)

	require.Nil(t, staleSnapshot)
	require.Len(t, freshSnapshot, 1)
	require.Equal(t, uint16(333), freshSnapshot[0].CPUTenths)
	require.Equal(t, uint16(444), freshSnapshot[0].MemoryTenths)
	require.Equal(t, uint64(555), freshSnapshot[0].MemoryUsageBytes)
}

func snapshotForTest(store *Store, containerID string, now time.Time) []containertypes.StatsHistorySample {
	store.mu.Lock()
	defer store.mu.Unlock()

	store.maybePruneLockedInternal(now)

	buffer := store.histories[containerID]
	if buffer == nil {
		return nil
	}

	return buffer.snapshot()
}
