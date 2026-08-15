//go:build !windows

package system

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGPUMonitorStatsCachedWithinTTL verifies that repeated Stats calls
// within gpuStatsTTL share one vendor-tool fork instead of launching
// nvidia-smi per call.
func TestGPUMonitorStatsCachedWithinTTL(t *testing.T) {
	binDir := t.TempDir()
	countFile := filepath.Join(binDir, "calls")

	script := "#!/bin/sh\necho run >> " + countFile + "\necho '0, Fake GPU, 512, 8192'\n"
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "nvidia-smi"), []byte(script), 0o755))
	t.Setenv("PATH", binDir)

	monitor := NewGPUMonitor(true, "nvidia")

	stats, err := monitor.Stats(context.Background())
	require.NoError(t, err)
	require.Len(t, stats, 1)
	require.Equal(t, "Fake GPU", stats[0].Name)
	require.EqualValues(t, 512*1024*1024, stats[0].MemoryUsed)

	again, err := monitor.Stats(context.Background())
	require.NoError(t, err)
	require.Equal(t, stats, again)

	invocations, err := os.ReadFile(countFile)
	require.NoError(t, err)
	require.Equal(t, "run\n", string(invocations), "nvidia-smi should have been forked exactly once within the TTL")
}
