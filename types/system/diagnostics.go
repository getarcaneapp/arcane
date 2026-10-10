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
	Goroutines         int       `json:"goroutines"`
	WSWorkerGoroutines int       `json:"wsWorkerGoroutines"`
	LeakedGoroutines   int       `json:"leakedGoroutines"`
	GOMAXPROCS         int       `json:"gomaxprocs"`
	NumCPU             int       `json:"numCpu"`
	GoVersion          string    `json:"goVersion"`
	OS                 string    `json:"os"`
	Arch               string    `json:"arch"`
	NumCgoCall         int64     `json:"numCgoCall"`
	UptimeSeconds      int64     `json:"uptimeSeconds"`
	LeakScannedAt      time.Time `json:"leakScannedAt,omitzero"`
}

// GoroutineLeakReport is the result of an on-demand goroutine leak scan. The
// scan triggers a leak-detection GC cycle, then writes the goroutineleak
// pprof profile as debug=1 text (leaked stacks only).
type GoroutineLeakReport struct {
	Count     int       `json:"count"`
	Profile   string    `json:"profile"`
	ScannedAt time.Time `json:"scannedAt"`
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

// ActorDiagnostics describes the Francis actor host of one environment.
type ActorDiagnostics struct {
	EnvironmentID   string `json:"environmentId"`
	EnvironmentName string `json:"environmentName"`
	// Error is set when the environment's actor diagnostics could not be collected.
	Error      string          `json:"error,omitempty"`
	Ready      bool            `json:"ready"`
	Hosts      []ActorHostInfo `json:"hosts"`
	ActorTypes []ActorTypeInfo `json:"actorTypes"`
	Runs       map[string]int  `json:"runs"`
	CheckedAt  time.Time       `json:"checkedAt"`
}

// ActorHostInfo is a registered Francis host.
type ActorHostInfo struct {
	HostID          string    `json:"hostId"`
	Address         string    `json:"address"`
	LastHealthCheck time.Time `json:"lastHealthCheck"`
}

// ActorTypeInfo holds per actor type activation, alarm, and job details.
type ActorTypeInfo struct {
	ActorType string `json:"actorType"`
	// ConcurrencyLimit caps concurrent invocations per actor; 0 is unlimited.
	ConcurrencyLimit int               `json:"concurrencyLimit"`
	ActiveActors     int               `json:"activeActors"`
	StoredStates     int               `json:"storedStates"`
	PendingAlarms    int               `json:"pendingAlarms"`
	PendingJobs      int               `json:"pendingJobs"`
	LeasedAlarms     int               `json:"leasedAlarms"`
	CompletedJobs    int               `json:"completedJobs"`
	DeadJobs         int               `json:"deadJobs"`
	NextAlarmAt      time.Time         `json:"nextAlarmAt,omitzero"`
	Instances        []ActorInstance   `json:"instances"`
	RecentFailures   []ActorJobFailure `json:"recentFailures"`
}

// ActorInstance is an activated actor.
type ActorInstance struct {
	ActorID string `json:"actorId"`
	// Name is Arcane's display name for the actor, when known.
	Name        string    `json:"name,omitempty"`
	HostID      string    `json:"hostId"`
	ActivatedAt time.Time `json:"activatedAt"`
}

// ActorJobFailure is a dead-lettered actor job.
type ActorJobFailure struct {
	JobID     string    `json:"jobId"`
	ActorID   string    `json:"actorId"`
	Name      string    `json:"name,omitempty"`
	Method    string    `json:"method"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"lastError,omitempty"`
	FailedAt  time.Time `json:"failedAt"`
}

// DiagnosticsCommand is a client request sent over the diagnostics stream.
type DiagnosticsCommand struct {
	ID string `json:"id"`
	// Type is refresh, dump, leakScan, or profile.
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
	Seconds int    `json:"seconds,omitempty"`
}

// DiagnosticsMessage is a snapshot push or command result on the diagnostics stream.
type DiagnosticsMessage struct {
	// Type is snapshot or result.
	Type       string               `json:"type"`
	ID         string               `json:"id,omitempty"`
	Snapshot   *Diagnostics         `json:"snapshot,omitempty"`
	LeakReport *GoroutineLeakReport `json:"leakReport,omitempty"`
	Text       string               `json:"text,omitempty"`
	Data       []byte               `json:"data,omitempty"`
	Error      string               `json:"error,omitempty"`
}
