package system

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	"kit.getarcane.app/cache"

	"getarcane.dev/types/v3"
)

type (
	GPUMonitor struct {
		statsGroup singleflight.Group

		detectionCache *cache.Cache[struct{}, gpuDetection]

		// statsCache holds the last vendor-tool result (success or failure) for
		// gpuStatsTTL so concurrent consumers and fast sampler ticks share one
		// process fork instead of each launching their own.
		statsCache     atomic.Pointer[gpuStatsSnapshot]
		configuredType string

		announcedType string

		detectionMu   sync.Mutex
		detectionDone bool

		enabled bool
	}

	gpuStatsSnapshot struct {
		sampledAt time.Time
		err       error
		stats     []types.GPUStats
	}

	gpuDetection struct {
		gpuType  string
		toolPath string
		detected bool
	}
)

const (
	AMDGPUSysfsPath = "/sys/class/drm"
	gpuDetectionTTL = 30 * time.Second
	// gpuStatsTTL bounds how often the vendor tool is invoked: reading VRAM
	// stats forks a process (nvidia-smi, intel_gpu_top), which is far too
	// expensive to repeat on every sampler tick.
	gpuStatsTTL = 3 * time.Second
)

func NewGPUMonitor(enabled bool, configuredType string) *GPUMonitor {
	return &GPUMonitor{
		enabled:        enabled,
		configuredType: configuredType,
		detectionCache: &cache.Cache[struct{}, gpuDetection]{
			Capacity: 1,
			TTL:      gpuDetectionTTL,
		},
	}
}

func (m *GPUMonitor) Enabled() bool { return m.enabled }

func (m *GPUMonitor) Stats(ctx context.Context) ([]types.GPUStats, error) {
	if !m.enabled {
		return nil, nil
	}

	if snapshot := m.statsCache.Load(); snapshot != nil && time.Since(snapshot.sampledAt) < gpuStatsTTL {
		return snapshot.stats, snapshot.err
	}

	result, err, _ := m.statsGroup.Do("stats", func() (any, error) {
		previous := m.statsCache.Load()
		if previous != nil && time.Since(previous.sampledAt) < gpuStatsTTL {
			return previous.stats, previous.err
		}
		stats, statsErr := m.statsUncached(ctx)
		if previousErr := kit.FromPtr(previous).err; statsErr != nil && (previousErr == nil || previousErr.Error() != statsErr.Error()) {
			slog.WarnContext(ctx, "GPU stats unavailable", "error", statsErr)
		}
		m.statsCache.Store(&gpuStatsSnapshot{stats: stats, err: statsErr, sampledAt: time.Now()})
		return stats, statsErr
	})
	stats, _ := result.([]types.GPUStats)
	return stats, err
}

func (m *GPUMonitor) statsUncached(ctx context.Context) ([]types.GPUStats, error) {
	m.detectionMu.Lock()
	done := m.detectionDone
	m.detectionMu.Unlock()
	if !done {
		if err := m.detect(ctx); err != nil {
			return nil, err
		}
	}

	if detection, found, _ := m.detectionCache.Get(struct{}{}); found && detection.detected {
		return m.statsForType(ctx, detection.gpuType)
	}

	if err := m.detect(ctx); err != nil {
		return nil, err
	}

	detection, found, _ := m.detectionCache.Get(struct{}{})
	if !found || detection.gpuType == "" {
		return nil, errors.New("no supported GPU found")
	}
	return m.statsForType(ctx, detection.gpuType)
}

func (m *GPUMonitor) statsForType(ctx context.Context, gpuType string) ([]types.GPUStats, error) {
	switch gpuType {
	case "nvidia":
		return getNvidiaStats(ctx)
	case "amd":
		return getAMDStats(ctx)
	case "intel":
		return getIntelStats(ctx)
	default:
		return nil, errors.New("no supported GPU found")
	}
}

func (m *GPUMonitor) markDetected(gpuType, toolPath string) bool {
	m.detectionCache.Set(cache.Entry[struct{}, gpuDetection]{
		Key:   struct{}{},
		Value: gpuDetection{detected: true, gpuType: gpuType, toolPath: toolPath},
	})
	m.detectionDone = true
	announce := m.announcedType != gpuType
	m.announcedType = gpuType
	return announce
}

func (m *GPUMonitor) detect(ctx context.Context) error {
	m.detectionMu.Lock()
	defer m.detectionMu.Unlock()

	if t := m.configuredType; t != "" && t != "auto" {
		switch t {
		case "nvidia":
			if path, err := exec.LookPath("nvidia-smi"); err == nil {
				if m.markDetected("nvidia", path) {
					slog.InfoContext(ctx, "Using configured GPU type", "type", "nvidia")
				}
				return nil
			}
			return errors.New("nvidia-smi not found but GPU_TYPE set to nvidia")
		case "amd":
			if HasAMDGPU(ctx) {
				if m.markDetected("amd", AMDGPUSysfsPath) {
					slog.InfoContext(ctx, "Using configured GPU type", "type", "amd")
				}
				return nil
			}
			return errors.New("AMD GPU not found in sysfs but GPU_TYPE set to amd")
		case "intel":
			if path, err := exec.LookPath("intel_gpu_top"); err == nil {
				if m.markDetected("intel", path) {
					slog.InfoContext(ctx, "Using configured GPU type", "type", "intel")
				}
				return nil
			}
			return errors.New("intel_gpu_top not found but GPU_TYPE set to intel")
		default:
			slog.WarnContext(ctx, "Invalid GPU_TYPE specified, falling back to auto-detection", "gpuType", t)
		}
	}

	if path, err := exec.LookPath("nvidia-smi"); err == nil {
		if m.markDetected("nvidia", path) {
			slog.InfoContext(ctx, "NVIDIA GPU detected", "tool", "nvidia-smi", "path", path)
		}
		return nil
	}
	if HasAMDGPU(ctx) {
		if m.markDetected("amd", AMDGPUSysfsPath) {
			slog.InfoContext(ctx, "AMD GPU detected", "method", "sysfs", "path", AMDGPUSysfsPath)
		}
		return nil
	}
	if path, err := exec.LookPath("intel_gpu_top"); err == nil {
		if m.markDetected("intel", path) {
			slog.InfoContext(ctx, "Intel GPU detected", "tool", "intel_gpu_top", "path", path)
		}
		return nil
	}

	m.detectionDone = true
	return errors.New("no supported GPU found")
}

func HasAMDGPU(ctx context.Context) bool {
	entries, err := acfs.Host{}.List(ctx, AMDGPUSysfsPath)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := entry.Name
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			continue
		}
		if exists, existsErr := (acfs.Host{}).Exists(ctx, fmt.Sprintf("%s/%s/device/mem_info_vram_total", AMDGPUSysfsPath, name)); existsErr == nil && exists {
			return true
		}
	}
	return false
}

func readSysfsValue(ctx context.Context, path string) (uint64, error) {
	data, err := acfs.Host{}.ReadFile(ctx, path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

func getNvidiaStats(ctx context.Context) ([]types.GPUStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=index,name,memory.used,memory.total",
		"--format=csv,noheader,nounits")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi execution failed: %w", err)
	}

	reader := csv.NewReader(bytes.NewReader(output))
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse nvidia-smi output: %w", err)
	}

	var stats []types.GPUStats
	for _, record := range records {
		if len(record) < 4 {
			continue
		}
		index, atoiErr := strconv.Atoi(strings.TrimSpace(record[0]))
		if atoiErr != nil {
			slog.WarnContext(ctx, "Failed to parse GPU index", "value", record[0])
			continue
		}
		memUsed, atoiErr := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
		if atoiErr != nil {
			slog.WarnContext(ctx, "Failed to parse memory used", "value", record[2])
			continue
		}
		memTotal, atoiErr := strconv.ParseFloat(strings.TrimSpace(record[3]), 64)
		if atoiErr != nil {
			slog.WarnContext(ctx, "Failed to parse memory total", "value", record[3])
			continue
		}
		stats = append(stats, types.GPUStats{
			Name:        strings.TrimSpace(record[1]),
			Index:       index,
			MemoryUsed:  memUsed * 1024 * 1024,
			MemoryTotal: memTotal * 1024 * 1024,
		})
	}

	if len(stats) == 0 {
		return nil, errors.New("no GPU data parsed from nvidia-smi")
	}

	slog.DebugContext(ctx, "Collected NVIDIA GPU stats", "gpuCount", len(stats))
	return stats, nil
}

func getAMDStats(ctx context.Context) ([]types.GPUStats, error) {
	entries, err := acfs.Host{}.List(ctx, AMDGPUSysfsPath)
	if err != nil {
		slog.WarnContext(ctx, "Failed to read DRM sysfs directory", "error", err)
		return nil, fmt.Errorf("failed to read sysfs: %w", err)
	}

	var stats []types.GPUStats
	index := 0
	for _, entry := range entries {
		name := entry.Name
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			continue
		}

		devicePath := fmt.Sprintf("%s/%s/device", AMDGPUSysfsPath, name)
		memTotalBytes, readSysfsValueErr := readSysfsValue(ctx, devicePath+"/mem_info_vram_total")
		if readSysfsValueErr != nil {
			continue
		}
		memUsedBytes, readSysfsValueErr := readSysfsValue(ctx, devicePath+"/mem_info_vram_used")
		if readSysfsValueErr != nil {
			slog.WarnContext(ctx, "Failed to read AMD GPU memory used", "card", name, "error", readSysfsValueErr)
			continue
		}

		stats = append(stats, types.GPUStats{
			Name:        fmt.Sprintf("AMD GPU %d", index),
			Index:       index,
			MemoryUsed:  float64(memUsedBytes),
			MemoryTotal: float64(memTotalBytes),
		})
		index++
	}

	if len(stats) == 0 {
		return nil, errors.New("no AMD GPU data found in sysfs")
	}

	slog.DebugContext(ctx, "Collected AMD GPU stats", "gpuCount", len(stats))
	return stats, nil
}

func getIntelStats(ctx context.Context) ([]types.GPUStats, error) {
	stats := []types.GPUStats{
		{Name: "Intel GPU", Index: 0, MemoryUsed: 0, MemoryTotal: 0},
	}
	slog.DebugContext(ctx, "Intel GPU detected but detailed stats not yet implemented")
	return stats, nil
}
