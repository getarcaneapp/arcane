package system

import (
	"context"
	"path/filepath"
	"testing"
)

func TestGetIntelStatsInternalReturnsUsableMemoryStats(t *testing.T) {
	originalSysfsPath := gpuSysfsPath
	gpuSysfsPath = filepath.Join("testdata", "intel-drm")
	t.Cleanup(func() { gpuSysfsPath = originalSysfsPath })

	stats, err := getIntelStatsInternal(context.Background())
	if err != nil {
		t.Fatalf("get Intel GPU stats: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected one Intel GPU, got %d", len(stats))
	}
	if stats[0].Index != 1 || stats[0].Name != "Intel GPU 1" {
		t.Fatalf("expected Intel GPU at card1, got %#v", stats[0])
	}
	if stats[0].MemoryUsed != 1073741824 || stats[0].MemoryTotal != 6442450944 {
		t.Fatalf("unexpected Intel GPU memory stats: %#v", stats[0])
	}
}

func TestGPUMonitorDetectsConfiguredIntelGPUFromSysfs(t *testing.T) {
	originalSysfsPath := gpuSysfsPath
	gpuSysfsPath = filepath.Join("testdata", "intel-drm")
	t.Cleanup(func() { gpuSysfsPath = originalSysfsPath })

	stats, err := NewGPUMonitor(true, "intel").Stats(context.Background())
	if err != nil {
		t.Fatalf("collect configured Intel GPU stats: %v", err)
	}
	if len(stats) != 1 || stats[0].MemoryTotal == 0 {
		t.Fatalf("expected usable Intel GPU stats, got %#v", stats)
	}
}
