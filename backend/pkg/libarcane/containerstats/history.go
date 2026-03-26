package containerstats

import (
	"math"
	"sync"
	"time"

	containertypes "github.com/getarcaneapp/arcane/types/container"
	dockercontainer "github.com/moby/moby/api/types/container"
)

const (
	HistoryCapacity      = 30
	historyTTL           = 10 * time.Minute
	historyPruneInterval = time.Minute
)

type Store struct {
	mu        sync.Mutex
	histories map[string]*historyBuffer
	lastPrune time.Time
}

type historyBuffer struct {
	samples   [HistoryCapacity]containertypes.StatsHistorySample
	start     int
	count     int
	updatedAt time.Time
}

func BuildSample(stats dockercontainer.StatsResponse) containertypes.StatsHistorySample {
	memoryUsage := calculateMemoryUsageInternal(stats)
	return containertypes.StatsHistorySample{
		CPUTenths:        percentToTenthsInternal(calculateCPUPercentInternal(stats)),
		MemoryTenths:     percentToTenthsInternal(calculateMemoryPercentInternal(stats)),
		MemoryUsageBytes: memoryUsage,
	}
}

func (s *Store) Record(containerID string, sample containertypes.StatsHistorySample, includeHistory bool, recordedAt time.Time) []containertypes.StatsHistorySample {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.histories == nil {
		s.histories = make(map[string]*historyBuffer)
	}

	s.maybePruneLockedInternal(recordedAt)

	buffer := s.histories[containerID]
	if buffer == nil {
		buffer = &historyBuffer{}
		s.histories[containerID] = buffer
	}

	buffer.append(sample, recordedAt)

	if !includeHistory {
		return nil
	}

	return buffer.snapshot()
}

func (b *historyBuffer) append(sample containertypes.StatsHistorySample, recordedAt time.Time) {
	if b.count < HistoryCapacity {
		index := (b.start + b.count) % HistoryCapacity
		b.samples[index] = sample
		b.count++
	} else {
		b.samples[b.start] = sample
		b.start = (b.start + 1) % HistoryCapacity
	}

	b.updatedAt = recordedAt
}

func (b *historyBuffer) snapshot() []containertypes.StatsHistorySample {
	if b.count == 0 {
		return nil
	}

	out := make([]containertypes.StatsHistorySample, 0, b.count)
	for i := range b.count {
		index := (b.start + i) % HistoryCapacity
		out = append(out, b.samples[index])
	}
	return out
}

func (s *Store) maybePruneLockedInternal(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}

	if !s.lastPrune.IsZero() && now.Sub(s.lastPrune) < historyPruneInterval {
		return
	}

	s.lastPrune = now
	for containerID, buffer := range s.histories {
		if buffer == nil || now.Sub(buffer.updatedAt) > historyTTL {
			delete(s.histories, containerID)
		}
	}
}

func calculateCPUPercentInternal(stats dockercontainer.StatsResponse) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)

	if systemDelta <= 0 || cpuDelta <= 0 {
		return 0
	}

	return math.Min(math.Max((cpuDelta/systemDelta)*100, 0), 100)
}

func calculateMemoryPercentInternal(stats dockercontainer.StatsResponse) float64 {
	limit := stats.MemoryStats.Limit
	if limit == 0 {
		return 0
	}

	usage := calculateMemoryUsageInternal(stats)
	return math.Min(math.Max((float64(usage)/float64(limit))*100, 0), 100)
}

func calculateMemoryUsageInternal(stats dockercontainer.StatsResponse) uint64 {
	usage := stats.MemoryStats.Usage
	inactiveFile := stats.MemoryStats.Stats["inactive_file"]
	if usage <= inactiveFile {
		return 0
	}

	return usage - inactiveFile
}

func percentToTenthsInternal(value float64) uint16 {
	clamped := math.Min(math.Max(value, 0), 100)
	return uint16(math.Round(clamped * 10))
}
