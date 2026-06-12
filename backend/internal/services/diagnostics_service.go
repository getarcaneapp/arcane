package services

import (
	"runtime"
	"runtime/debug"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/system"
)

// recentGCPauseSamples is the number of recent GC pause durations reported.
const recentGCPauseSamples = 16

// DiagnosticsService gathers Go runtime, memory, and garbage-collector
// statistics for the diagnostics endpoints. It holds no external dependencies;
// WebSocket metrics and worker-goroutine counts are merged in at the handler
// layer to avoid an import cycle with the api/ws package.
type DiagnosticsService struct {
	startedAt time.Time
}

// NewDiagnosticsService returns a DiagnosticsService. startedAt is captured at
// construction (≈ process start) and used to report uptime.
func NewDiagnosticsService() *DiagnosticsService {
	return &DiagnosticsService{startedAt: time.Now()}
}

// Collect samples the current runtime, memory, and GC state.
func (s *DiagnosticsService) Collect() (system.RuntimeInfo, system.MemoryInfo, system.GCInfo) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	var gc debug.GCStats
	debug.ReadGCStats(&gc)

	rt := system.RuntimeInfo{
		Goroutines:    runtime.NumGoroutine(),
		GOMAXPROCS:    runtime.GOMAXPROCS(0),
		NumCPU:        runtime.NumCPU(),
		GoVersion:     runtime.Version(),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		NumCgoCall:    runtime.NumCgoCall(),
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
	}

	mi := system.MemoryInfo{
		Alloc:         mem.Alloc,
		TotalAlloc:    mem.TotalAlloc,
		Sys:           mem.Sys,
		HeapAlloc:     mem.HeapAlloc,
		HeapSys:       mem.HeapSys,
		HeapInuse:     mem.HeapInuse,
		HeapIdle:      mem.HeapIdle,
		HeapReleased:  mem.HeapReleased,
		HeapObjects:   mem.HeapObjects,
		StackInuse:    mem.StackInuse,
		StackSys:      mem.StackSys,
		MSpanInuse:    mem.MSpanInuse,
		MCacheInuse:   mem.MCacheInuse,
		NextGC:        mem.NextGC,
		NumGC:         mem.NumGC,
		NumForcedGC:   mem.NumForcedGC,
		GCCPUFraction: mem.GCCPUFraction,
	}

	// gc.Pause is ordered most-recent-first; cap the slice we expose.
	pauses := gc.Pause
	if len(pauses) > recentGCPauseSamples {
		pauses = pauses[:recentGCPauseSamples]
	}
	recent := make([]int64, len(pauses))
	for i, p := range pauses {
		recent[i] = p.Nanoseconds()
	}

	gi := system.GCInfo{
		LastGC:         gc.LastGC,
		NumGC:          gc.NumGC,
		PauseTotalNs:   gc.PauseTotal.Nanoseconds(),
		RecentPausesNs: recent,
	}

	return rt, mi, gi
}
