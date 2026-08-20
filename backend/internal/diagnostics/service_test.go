package diagnostics

import (
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticsServiceCollect(t *testing.T) {
	s := NewDiagnosticsService()

	rt, mem, _ := s.Collect()

	if rt.Goroutines <= 0 {
		assert.Positive(t, rt.Goroutines,
			"expected a positive goroutine count, got %d", rt.Goroutines)
	}

	assert.NotEmpty(t, rt.GoVersion,
		"expected a non-empty Go version")

	if rt.NumCPU <= 0 {
		assert.Positive(t, rt.NumCPU,
			"expected a positive CPU count, got %d", rt.NumCPU)
	}

	assert.NotEqual(t, 0, mem.Sys,
		"expected non-zero Sys memory")

	assert.GreaterOrEqual(t, rt.LeakedGoroutines, 0)
	assert.True(t, rt.LeakScannedAt.IsZero(),
		"expected no leak scan before ScanGoroutineLeaks")
}

func TestGoroutineLeakProfileAvailable(t *testing.T) {
	assert.NotNil(t, pprof.Lookup(goroutineLeakProfileName),
		"goroutineleak pprof profile must be registered")
}

func TestDiagnosticsServiceScanGoroutineLeaks(t *testing.T) {
	s := NewDiagnosticsService()

	report, err := s.ScanGoroutineLeaks()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, report.Count, 0)
	assert.False(t, report.ScannedAt.IsZero())
	assert.Contains(t, report.Profile, "goroutineleak profile")

	rt, _, _ := s.Collect()
	assert.Equal(t, report.Count, rt.LeakedGoroutines)
	assert.False(t, rt.LeakScannedAt.IsZero())
	assert.True(t, rt.LeakScannedAt.Equal(report.ScannedAt))
}

func TestDiagnosticsServiceScanGoroutineLeaksDetectsLeak(t *testing.T) {
	s := NewDiagnosticsService()
	leakUnbufferedChannelInternal()

	deadline := time.Now().Add(5 * time.Second)
	var reportCount int
	var reportProfile string
	for {
		report, err := s.ScanGoroutineLeaks()
		require.NoError(t, err)
		reportCount = report.Count
		reportProfile = report.Profile
		if report.Count >= 1 && strings.Contains(report.Profile, "blockForeverOnDroppedChannelInternal") {
			return
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected a leaked goroutine in the goroutineleak profile, count=%d profile=%s",
		reportCount, reportProfile)
}

// leakUnbufferedChannelInternal starts a goroutine blocked on a channel that
// becomes unreachable, which the runtime classifies as a leak.
func leakUnbufferedChannelInternal() {
	go blockForeverOnDroppedChannelInternal()
	for range 10 {
		runtime.Gosched()
	}
}

//go:noinline
func blockForeverOnDroppedChannelInternal() {
	<-make(chan struct{})
}
