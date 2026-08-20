package diagnostics

import (
	"bytes"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/types/v2/system"
)

// recentGCPauseSamples is the number of recent GC pause durations reported.
const recentGCPauseSamples = 16

// goroutineLeakProfileName is the runtime/pprof profile that reports goroutines
// blocked on concurrency primitives that cannot be unblocked.
const goroutineLeakProfileName = "goroutineleak"

// DiagnosticsService gathers Go runtime, memory, and garbage-collector
// statistics for the diagnostics endpoints. It holds no external dependencies;
// WebSocket metrics and worker-goroutine counts are merged in at the handler
// layer to avoid an import cycle with the api/ws package.
type DiagnosticsService struct {
	startedAt time.Time

	leakMu        sync.Mutex
	leakScannedAt time.Time
}

// NewDiagnosticsService returns a DiagnosticsService. startedAt is captured at
// construction (≈ process start) and used to report uptime.
func NewDiagnosticsService() *DiagnosticsService {
	return &DiagnosticsService{startedAt: time.Now()}
}

// Collect samples the current runtime, memory, and GC state.
//
// LeakedGoroutines is the last-known leak count from the runtime; it does not
// trigger a leak-detection GC. Use ScanGoroutineLeaks for a fresh scan.
func (s *DiagnosticsService) Collect() (system.RuntimeInfo, system.MemoryInfo, system.GCInfo) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	var gc debug.GCStats
	debug.ReadGCStats(&gc)

	s.leakMu.Lock()
	leakScannedAt := s.leakScannedAt
	s.leakMu.Unlock()

	rt := system.RuntimeInfo{
		Goroutines:       runtime.NumGoroutine(),
		LeakedGoroutines: leakedGoroutineCountInternal(),
		LeakScannedAt:    leakScannedAt,
		GOMAXPROCS:       runtime.GOMAXPROCS(0),
		NumCPU:           runtime.NumCPU(),
		GoVersion:        runtime.Version(),
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		NumCgoCall:       runtime.NumCgoCall(),
		UptimeSeconds:    int64(time.Since(s.startedAt).Seconds()),
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

// ScanGoroutineLeaks runs a leak-detection GC and returns the leaked-goroutine
// pprof text (debug=1). debug=2 would dump every goroutine, not only leaks.
func (s *DiagnosticsService) ScanGoroutineLeaks() (system.GoroutineLeakReport, error) {
	p := pprof.Lookup(goroutineLeakProfileName)
	if p == nil {
		return system.GoroutineLeakReport{}, errors.New("goroutineleak profile is not available")
	}

	var buf bytes.Buffer
	if err := p.WriteTo(&buf, 1); err != nil {
		return system.GoroutineLeakReport{}, errors.WrapIf(err, "write goroutineleak profile")
	}

	now := time.Now().UTC()
	s.leakMu.Lock()
	s.leakScannedAt = now
	s.leakMu.Unlock()

	return system.GoroutineLeakReport{
		Count:     p.Count(),
		Profile:   buf.String(),
		ScannedAt: now,
	}, nil
}

// leakedGoroutineCountInternal returns the last-known leak count without
// triggering a leak-detection GC.
func leakedGoroutineCountInternal() int {
	p := pprof.Lookup(goroutineLeakProfileName)
	if p == nil {
		return 0
	}
	return p.Count()
}
