package system

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
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
		m.cacheMu.RLock()
		toolPath := m.toolPath
		m.cacheMu.RUnlock()
		return getIntelStatsInternal(ctx, toolPath)
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
			// intel_gpu_top absent — fall back to sysfs so the GPU still shows in the
			// dashboard (ARM builds, minimal containers, Proxmox passthrough).
			if HasIntelGPU() {
				m.markDetectedInternal("intel", "")
				slog.InfoContext(ctx, "Using configured GPU type via sysfs (intel_gpu_top not found)", "type", "intel")
				return nil
			}
			return fmt.Errorf("intel_gpu_top not found and no Intel GPU detected in sysfs, but GPU_TYPE set to intel")
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
	// Last resort: sysfs vendor-ID detection so the GPU shows up even without the tool.
	if HasIntelGPU() {
		m.markDetectedInternal("intel", "")
		slog.InfoContext(ctx, "Intel GPU detected", "method", "sysfs")
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

// intelGPUTopOutput is the subset of intel_gpu_top JSON we care about.
// The memory block is only present on discrete GPUs (Intel Arc and later).
type intelGPUTopOutput struct {
	Memory *struct {
		Unit  string `json:"unit"`
		Local *struct {
			Total float64 `json:"total"`
			Free  float64 `json:"free"`
		} `json:"local"`
	} `json:"memory"`
}

// findIntelDRICardsInternal returns /dev/dri/cardN paths for cards whose PCI vendor
// is Intel (0x8086). This handles Proxmox and similar setups where card0 is a
// VirtIO display adapter and the real Arc GPU sits on card1 or higher.
func findIntelDRICardsInternal() []string {
	entries, err := os.ReadDir(AMDGPUSysfsPath)
	if err != nil {
		return nil
	}
	var cards []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			continue
		}
		data, err := os.ReadFile(fmt.Sprintf("%s/%s/device/vendor", AMDGPUSysfsPath, name))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) == "0x8086" {
			cards = append(cards, fmt.Sprintf("/dev/dri/%s", name))
		}
	}
	return cards
}

// HasIntelGPU reports whether at least one Intel GPU is present via DRM sysfs.
func HasIntelGPU() bool {
	return len(findIntelDRICardsInternal()) > 0
}

// getIntelStatsInternal iterates over every Intel DRI card and collects VRAM stats.
// toolPath is the path to intel_gpu_top (empty string → sysfs-only, no memory stats).
func getIntelStatsInternal(ctx context.Context, toolPath string) ([]systemtypes.GPUStats, error) {
	intelCards := findIntelDRICardsInternal()
	if len(intelCards) == 0 {
		return nil, fmt.Errorf("no Intel GPU found in sysfs")
	}

	var stats []systemtypes.GPUStats
	for i, cardPath := range intelCards {
		entry := systemtypes.GPUStats{
			Name:  intelGPUNameInternal(cardPath),
			Index: i,
		}
		if toolPath != "" {
			if mem, err := intelGPUTopMemoryInternal(ctx, toolPath, cardPath); err == nil {
				entry.MemoryUsed = mem.used
				entry.MemoryTotal = mem.total
			} else {
				slog.DebugContext(ctx, "intel_gpu_top memory query failed", "card", cardPath, "error", err)
			}
		}
		stats = append(stats, entry)
	}

	slog.DebugContext(ctx, "Collected Intel GPU stats", "gpu_count", len(stats))
	return stats, nil
}

type intelMemStats struct{ used, total float64 }

// intelGPUTopMemoryInternal runs intel_gpu_top for a single card and returns VRAM stats.
func intelGPUTopMemoryInternal(ctx context.Context, toolPath, cardPath string) (intelMemStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, toolPath, //nolint:gosec // toolPath from exec.LookPath, cardPath from sysfs vendor check
		"-d", fmt.Sprintf("drm:%s", cardPath),
		"-J", "-s", "100", "-c", "1")
	out, err := cmd.Output()
	if err != nil {
		return intelMemStats{}, fmt.Errorf("intel_gpu_top: %w", err)
	}

	var result intelGPUTopOutput
	data := bytes.TrimSpace(out)
	if len(data) > 0 && data[0] == '[' {
		var arr []intelGPUTopOutput
		if err := json.Unmarshal(data, &arr); err != nil {
			return intelMemStats{}, fmt.Errorf("parse intel_gpu_top array: %w", err)
		}
		if len(arr) == 0 {
			return intelMemStats{}, fmt.Errorf("parse intel_gpu_top array: empty output")
		}
		result = arr[0]
	} else {
		if err := json.Unmarshal(data, &result); err != nil {
			return intelMemStats{}, fmt.Errorf("parse intel_gpu_top object: %w", err)
		}
	}

	if result.Memory == nil || result.Memory.Local == nil {
		return intelMemStats{}, fmt.Errorf("no local memory info in intel_gpu_top output")
	}

	unit := strings.ToLower(strings.TrimSpace(result.Memory.Unit))
	var scale float64
	switch unit {
	case "mib":
		scale = 1024 * 1024
	case "gib":
		scale = 1024 * 1024 * 1024
	default:
		slog.WarnContext(ctx, "Unknown intel_gpu_top memory unit, treating as bytes", "unit", unit)
		scale = 1
	}

	total := result.Memory.Local.Total * scale
	used := (result.Memory.Local.Total - result.Memory.Local.Free) * scale
	return intelMemStats{used: used, total: total}, nil
}

// intelGPUNameInternal returns a human-readable label for an Intel DRI card.
// Prefers the DRM "label" sysfs attribute; falls back to "Intel GPU (cardN)".
func intelGPUNameInternal(cardPath string) string {
	cardName := strings.TrimPrefix(cardPath, "/dev/dri/")
	if data, err := os.ReadFile(fmt.Sprintf("%s/%s/device/label", AMDGPUSysfsPath, cardName)); err == nil {
		if label := strings.TrimSpace(string(data)); label != "" {
			return label
		}
	}
	return fmt.Sprintf("Intel GPU (%s)", cardName)
}
