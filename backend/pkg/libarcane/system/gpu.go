package system

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	systemtypes "github.com/getarcaneapp/arcane/types/system"
)

// AMDGPUSysfsPath is the sysfs base used to discover AMD GPUs.
const AMDGPUSysfsPath = "/sys/class/drm"

// gpuDetectionTTL bounds how long a successful detection result is reused before re-detecting.
const gpuDetectionTTL = 30 * time.Second

// GPUMonitor probes for an attached GPU (NVIDIA / AMD / Intel) and reports VRAM usage.
// Detection is cached for gpuDetectionTTL; once a vendor is detected, subsequent Stats
// calls invoke the vendor-specific tool directly.
type GPUMonitor struct {
	enabled        bool
	configuredType string

	detectionMu   sync.Mutex
	detectionDone bool

	cacheMu   sync.RWMutex
	detected  bool
	timestamp time.Time
	gpuType   string
	toolPath  string
}

// NewGPUMonitor creates a monitor. enabled gates Stats; when false, Stats returns
// (nil, nil). configuredType is the user-pinned vendor ("nvidia"|"amd"|"intel"|"auto"|"")
// — anything else falls back to auto-detection.
func NewGPUMonitor(enabled bool, configuredType string) *GPUMonitor {
	return &GPUMonitor{enabled: enabled, configuredType: configuredType}
}

// Enabled reports whether GPU monitoring is on.
func (m *GPUMonitor) Enabled() bool { return m.enabled }

// Stats returns per-GPU VRAM stats. Returns (nil, 0, nil) when monitoring is disabled
// or no GPU is detected; vendor-specific errors are propagated otherwise.
func (m *GPUMonitor) Stats(ctx context.Context) ([]systemtypes.GPUStats, error) {
	if !m.enabled {
		return nil, nil
	}

	m.detectionMu.Lock()
	done := m.detectionDone
	m.detectionMu.Unlock()
	if !done {
		if err := m.detectInternal(ctx); err != nil {
			return nil, err
		}
	}

	m.cacheMu.RLock()
	if m.detected && time.Since(m.timestamp) < gpuDetectionTTL {
		t := m.gpuType
		m.cacheMu.RUnlock()
		return m.statsForTypeInternal(ctx, t)
	}
	m.cacheMu.RUnlock()

	if err := m.detectInternal(ctx); err != nil {
		return nil, err
	}

	m.cacheMu.RLock()
	t := m.gpuType
	m.cacheMu.RUnlock()
	if t == "" {
		return nil, fmt.Errorf("no supported GPU found")
	}
	return m.statsForTypeInternal(ctx, t)
}

func (m *GPUMonitor) statsForTypeInternal(ctx context.Context, gpuType string) ([]systemtypes.GPUStats, error) {
	switch gpuType {
	case "nvidia":
		return getNvidiaStatsInternal(ctx)
	case "amd":
		return getAMDStatsInternal(ctx)
	case "intel":
		return getIntelStatsInternal(ctx)
	default:
		return nil, fmt.Errorf("no supported GPU found")
	}
}

// markDetected records a successful detection. Caller must NOT hold cacheMu.
func (m *GPUMonitor) markDetectedInternal(gpuType, toolPath string) {
	m.cacheMu.Lock()
	m.detected = true
	m.gpuType = gpuType
	m.toolPath = toolPath
	m.timestamp = time.Now()
	m.cacheMu.Unlock()
	m.detectionDone = true
}

// detect runs vendor probing under detectionMu. The configuredType pin is honored when set
// to a known vendor; otherwise vendors are tried in order: nvidia → amd → intel.
func (m *GPUMonitor) detectInternal(ctx context.Context) error {
	m.detectionMu.Lock()
	defer m.detectionMu.Unlock()

	if t := m.configuredType; t != "" && t != "auto" {
		switch t {
		case "nvidia":
			if path, err := exec.LookPath("nvidia-smi"); err == nil {
				m.markDetectedInternal("nvidia", path)
				slog.InfoContext(ctx, "Using configured GPU type", "type", "nvidia")
				return nil
			}
			return fmt.Errorf("nvidia-smi not found but GPU_TYPE set to nvidia")
		case "amd":
			if HasAMDGPU() {
				m.markDetectedInternal("amd", AMDGPUSysfsPath)
				slog.InfoContext(ctx, "Using configured GPU type", "type", "amd")
				return nil
			}
			return fmt.Errorf("AMD GPU not found in sysfs but GPU_TYPE set to amd")
		case "intel":
			if path, err := exec.LookPath("intel_gpu_top"); err == nil {
				m.markDetectedInternal("intel", path)
				slog.InfoContext(ctx, "Using configured GPU type", "type", "intel")
				return nil
			}
			return fmt.Errorf("intel_gpu_top not found but GPU_TYPE set to intel")
		default:
			slog.WarnContext(ctx, "Invalid GPU_TYPE specified, falling back to auto-detection", "gpu_type", t)
		}
	}

	if path, err := exec.LookPath("nvidia-smi"); err == nil {
		m.markDetectedInternal("nvidia", path)
		slog.InfoContext(ctx, "NVIDIA GPU detected", "tool", "nvidia-smi", "path", path)
		return nil
	}
	if HasAMDGPU() {
		m.markDetectedInternal("amd", AMDGPUSysfsPath)
		slog.InfoContext(ctx, "AMD GPU detected", "method", "sysfs", "path", AMDGPUSysfsPath)
		return nil
	}
	if path, err := exec.LookPath("intel_gpu_top"); err == nil {
		m.markDetectedInternal("intel", path)
		slog.InfoContext(ctx, "Intel GPU detected", "tool", "intel_gpu_top", "path", path)
		return nil
	}

	m.detectionDone = true
	return fmt.Errorf("no supported GPU found")
}

// HasAMDGPU reports whether a card with mem_info_vram_total exists under AMDGPUSysfsPath.
func HasAMDGPU() bool {
	entries, err := os.ReadDir(AMDGPUSysfsPath)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			continue
		}
		if _, err := os.Stat(fmt.Sprintf("%s/%s/device/mem_info_vram_total", AMDGPUSysfsPath, name)); err == nil {
			return true
		}
	}
	return false
}

// readSysfsValueInternal parses a numeric value from a sysfs file.
func readSysfsValueInternal(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

func getNvidiaStatsInternal(ctx context.Context) ([]systemtypes.GPUStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=index,name,memory.used,memory.total",
		"--format=csv,noheader,nounits")

	output, err := cmd.Output()
	if err != nil {
		slog.WarnContext(ctx, "Failed to execute nvidia-smi", "error", err)
		return nil, fmt.Errorf("nvidia-smi execution failed: %w", err)
	}

	reader := csv.NewReader(bytes.NewReader(output))
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse nvidia-smi CSV output", "error", err)
		return nil, fmt.Errorf("failed to parse nvidia-smi output: %w", err)
	}

	var stats []systemtypes.GPUStats
	for _, record := range records {
		if len(record) < 4 {
			continue
		}
		index, err := strconv.Atoi(strings.TrimSpace(record[0]))
		if err != nil {
			slog.WarnContext(ctx, "Failed to parse GPU index", "value", record[0])
			continue
		}
		memUsed, err := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
		if err != nil {
			slog.WarnContext(ctx, "Failed to parse memory used", "value", record[2])
			continue
		}
		memTotal, err := strconv.ParseFloat(strings.TrimSpace(record[3]), 64)
		if err != nil {
			slog.WarnContext(ctx, "Failed to parse memory total", "value", record[3])
			continue
		}
		stats = append(stats, systemtypes.GPUStats{
			Name:        strings.TrimSpace(record[1]),
			Index:       index,
			MemoryUsed:  memUsed * 1024 * 1024,
			MemoryTotal: memTotal * 1024 * 1024,
		})
	}

	if len(stats) == 0 {
		return nil, fmt.Errorf("no GPU data parsed from nvidia-smi")
	}

	slog.DebugContext(ctx, "Collected NVIDIA GPU stats", "gpu_count", len(stats))
	return stats, nil
}

func getAMDStatsInternal(ctx context.Context) ([]systemtypes.GPUStats, error) {
	entries, err := os.ReadDir(AMDGPUSysfsPath)
	if err != nil {
		slog.WarnContext(ctx, "Failed to read DRM sysfs directory", "error", err)
		return nil, fmt.Errorf("failed to read sysfs: %w", err)
	}

	var stats []systemtypes.GPUStats
	index := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			continue
		}

		devicePath := fmt.Sprintf("%s/%s/device", AMDGPUSysfsPath, name)
		memTotalBytes, err := readSysfsValueInternal(fmt.Sprintf("%s/mem_info_vram_total", devicePath))
		if err != nil {
			continue
		}
		memUsedBytes, err := readSysfsValueInternal(fmt.Sprintf("%s/mem_info_vram_used", devicePath))
		if err != nil {
			slog.WarnContext(ctx, "Failed to read AMD GPU memory used", "card", name, "error", err)
			continue
		}

		stats = append(stats, systemtypes.GPUStats{
			Name:        fmt.Sprintf("AMD GPU %d", index),
			Index:       index,
			MemoryUsed:  float64(memUsedBytes),
			MemoryTotal: float64(memTotalBytes),
		})
		index++
	}

	if len(stats) == 0 {
		return nil, fmt.Errorf("no AMD GPU data found in sysfs")
	}

	slog.DebugContext(ctx, "Collected AMD GPU stats", "gpu_count", len(stats))
	return stats, nil
}

func getIntelStatsInternal(ctx context.Context) ([]systemtypes.GPUStats, error) {
	stats := []systemtypes.GPUStats{
		{Name: "Intel GPU", Index: 0, MemoryUsed: 0, MemoryTotal: 0},
	}
	slog.DebugContext(ctx, "Intel GPU detected but detailed stats not yet implemented")
	return stats, nil
}
