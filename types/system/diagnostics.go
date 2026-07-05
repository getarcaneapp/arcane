package system

import "time"

// Diagnostics is a point-in-time snapshot of the Go runtime, garbage collector,
// and active WebSocket connections. It is returned by the diagnostics REST
// endpoint and pushed over the live diagnostics WebSocket stream.
type Diagnostics struct {
	// Timestamp is when the snapshot was taken.
	//
	// Required: true
	Timestamp time.Time `json:"timestamp"`
	// Runtime holds Go runtime and scheduler counters.
	//
	// Required: true
	Runtime RuntimeInfo `json:"runtime"`
	// Memory holds a subset of runtime.MemStats.
	//
	// Required: true
	Memory MemoryInfo `json:"memory"`
	// GC holds garbage-collector statistics.
	//
	// Required: true
	GC GCInfo `json:"gc"`
	// WebSocket holds active WebSocket connection metrics.
	//
	// Required: true
	WebSocket WebSocketDiagnostics `json:"websocket"`
}

// RuntimeInfo describes the Go runtime, build, and scheduler state.
type RuntimeInfo struct {
	Goroutines         int    `json:"goroutines"`
	WSWorkerGoroutines int    `json:"wsWorkerGoroutines"`
	GOMAXPROCS         int    `json:"gomaxprocs"`
	NumCPU             int    `json:"numCpu"`
	GoVersion          string `json:"goVersion"`
	OS                 string `json:"os"`
	Arch               string `json:"arch"`
	NumCgoCall         int64  `json:"numCgoCall"`
	UptimeSeconds      int64  `json:"uptimeSeconds"`
}

// MemoryInfo is the subset of runtime.MemStats surfaced in diagnostics. All
// byte counts are raw bytes.
type MemoryInfo struct {
	Alloc         uint64  `json:"alloc"`
	TotalAlloc    uint64  `json:"totalAlloc"`
	Sys           uint64  `json:"sys"`
	HeapAlloc     uint64  `json:"heapAlloc"`
	HeapSys       uint64  `json:"heapSys"`
	HeapInuse     uint64  `json:"heapInuse"`
	HeapIdle      uint64  `json:"heapIdle"`
	HeapReleased  uint64  `json:"heapReleased"`
	HeapObjects   uint64  `json:"heapObjects"`
	StackInuse    uint64  `json:"stackInuse"`
	StackSys      uint64  `json:"stackSys"`
	MSpanInuse    uint64  `json:"mspanInuse"`
	MCacheInuse   uint64  `json:"mcacheInuse"`
	NextGC        uint64  `json:"nextGc"`
	NumGC         uint32  `json:"numGc"`
	NumForcedGC   uint32  `json:"numForcedGc"`
	GCCPUFraction float64 `json:"gcCpuFraction"`
}

// GCInfo holds garbage-collector statistics from runtime/debug.ReadGCStats.
type GCInfo struct {
	// LastGC is the time of the most recent collection.
	LastGC time.Time `json:"lastGc"`
	// NumGC is the total number of completed GC cycles.
	NumGC int64 `json:"numGc"`
	// PauseTotalNs is the cumulative stop-the-world pause time in nanoseconds.
	PauseTotalNs int64 `json:"pauseTotalNs"`
	// RecentPausesNs lists the most recent GC pause durations (ns), newest first.
	RecentPausesNs []int64 `json:"recentPausesNs"`
}

// WebSocketDiagnostics aggregates the active WebSocket connection counts and the
// list of currently-tracked connections.
type WebSocketDiagnostics struct {
	Snapshot    WebSocketMetricsSnapshot  `json:"snapshot"`
	Connections []WebSocketConnectionInfo `json:"connections"`
}
