package types

import (
	stdjson "encoding/json"
	"encoding/json/v2"
	"time"
)

// Diagnostics is a point-in-time snapshot of the Go runtime, garbage collector,
// and active WebSocket connections. It is returned by the diagnostics REST
// endpoint and pushed over the live diagnostics WebSocket stream.
type Diagnostics struct {
	// Timestamp is when the snapshot was taken.
	//
	// Required: true
	Timestamp time.Time `json:"timestamp"`
	// GC holds garbage-collector statistics.
	//
	// Required: true
	GC GCInfo `json:"gc"`
	// Runtime holds Go runtime and scheduler counters.
	//
	// Required: true
	Runtime RuntimeInfo `json:"runtime"`
	// WebSocket holds active WebSocket connection metrics.
	//
	// Required: true
	WebSocket WebSocketDiagnostics `json:"websocket"`
	// Memory holds a subset of runtime.MemStats.
	//
	// Required: true
	Memory MemoryInfo `json:"memory"`
}

// RuntimeInfo describes the Go runtime, build, and scheduler state.
type RuntimeInfo struct {
	LeakScannedAt      time.Time `json:"leakScannedAt,omitzero"`
	GoVersion          string    `json:"goVersion"`
	OS                 string    `json:"os"`
	Arch               string    `json:"arch"`
	Goroutines         int       `json:"goroutines"`
	WSWorkerGoroutines int       `json:"wsWorkerGoroutines"`
	LeakedGoroutines   int       `json:"leakedGoroutines"`
	GOMAXPROCS         int       `json:"gomaxprocs"`
	NumCPU             int       `json:"numCpu"`
	NumCgoCall         int64     `json:"numCgoCall"`
	UptimeSeconds      int64     `json:"uptimeSeconds"`
}

// GoroutineLeakReport is the result of an on-demand goroutine leak scan. The
// scan triggers a leak-detection GC cycle, then writes the goroutineleak
// pprof profile as debug=1 text (leaked stacks only).
type GoroutineLeakReport struct {
	ScannedAt time.Time `json:"scannedAt"`
	Profile   string    `json:"profile"`
	Count     int       `json:"count"`
}

// GoroutineProfile groups the live goroutines by identical stack, largest group first.
type GoroutineProfile struct {
	CapturedAt time.Time        `json:"capturedAt"`
	Groups     []GoroutineGroup `json:"groups"`
	Total      int              `json:"total"`
}

// GoroutineGroup is a set of goroutines that share one stack.
type GoroutineGroup struct {
	// Label is the outermost non-runtime function, usually the goroutine's entry point.
	Label string `json:"label"`
	// Frames lists the stack innermost first.
	Frames []GoroutineFrame `json:"frames"`
	Count  int              `json:"count"`
}

// GoroutineFrame is one stack frame of a goroutine group.
type GoroutineFrame struct {
	Function string `json:"function"`
	File     string `json:"file"`
	Line     int    `json:"line"`
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
	// RecentPausesNs lists the most recent GC pause durations (ns), newest first.
	RecentPausesNs []int64 `json:"recentPausesNs"`
	// NumGC is the total number of completed GC cycles.
	NumGC int64 `json:"numGc"`
	// PauseTotalNs is the cumulative stop-the-world pause time in nanoseconds.
	PauseTotalNs int64 `json:"pauseTotalNs"`
}

// WebSocketDiagnostics aggregates the active WebSocket connection counts and the
// list of currently-tracked connections.
type WebSocketDiagnostics struct {
	Connections []WebSocketConnectionInfo `json:"connections"`
	Snapshot    WebSocketMetricsSnapshot  `json:"snapshot"`
}

// ActorDiagnostics describes the Francis actor host of one environment.
type ActorDiagnostics struct {
	CheckedAt       time.Time      `json:"checkedAt"`
	Runs            map[string]int `json:"runs"`
	EnvironmentID   string         `json:"environmentId"`
	EnvironmentName string         `json:"environmentName"`
	// Error is set when the environment's actor diagnostics could not be collected.
	Error      string          `json:"error,omitempty"`
	Hosts      []ActorHostInfo `json:"hosts"`
	ActorTypes []ActorTypeInfo `json:"actorTypes"`
	Ready      bool            `json:"ready"`
}

// ActorHostInfo is a registered Francis host.
type ActorHostInfo struct {
	LastHealthCheck time.Time `json:"lastHealthCheck"`
	HostID          string    `json:"hostId"`
	Address         string    `json:"address"`
}

// ActorTypeInfo holds per actor type activation, alarm, and job details.
type ActorTypeInfo struct {
	NextAlarmAt    time.Time         `json:"nextAlarmAt,omitzero"`
	ActorType      string            `json:"actorType"`
	Instances      []ActorInstance   `json:"instances"`
	RecentFailures []ActorJobFailure `json:"recentFailures"`
	// ConcurrencyLimit caps concurrent invocations per actor; 0 is unlimited.
	ConcurrencyLimit int `json:"concurrencyLimit"`
	ActiveActors     int `json:"activeActors"`
	StoredStates     int `json:"storedStates"`
	PendingAlarms    int `json:"pendingAlarms"`
	PendingJobs      int `json:"pendingJobs"`
	LeasedAlarms     int `json:"leasedAlarms"`
	CompletedJobs    int `json:"completedJobs"`
	DeadJobs         int `json:"deadJobs"`
}

// ActorInstance is an activated actor.
type ActorInstance struct {
	ActivatedAt time.Time `json:"activatedAt"`
	ActorID     string    `json:"actorId"`
	// Name is Arcane's display name for the actor, when known.
	Name   string `json:"name,omitempty"`
	HostID string `json:"hostId"`
}

// ActorJobFailure is a dead-lettered actor job.
type ActorJobFailure struct {
	FailedAt  time.Time `json:"failedAt"`
	JobID     string    `json:"jobId"`
	ActorID   string    `json:"actorId"`
	Name      string    `json:"name,omitempty"`
	Method    string    `json:"method"`
	LastError string    `json:"lastError,omitempty"`
	Attempts  int       `json:"attempts"`
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
	Snapshot   *Diagnostics         `json:"snapshot,omitempty"`
	LeakReport *GoroutineLeakReport `json:"leakReport,omitempty"`
	// Type is snapshot or result.
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
	Data  []byte `json:"data,omitempty"`
}

type DockerRunCommand struct {
	Image       string   `json:"image"`
	Name        string   `json:"name,omitempty"`
	Restart     string   `json:"restart,omitempty"`
	Workdir     string   `json:"workdir,omitempty"`
	User        string   `json:"user,omitempty"`
	Entrypoint  string   `json:"entrypoint,omitempty"`
	Command     string   `json:"command,omitempty"`
	HealthCheck string   `json:"healthCheck,omitempty"`
	MemoryLimit string   `json:"memoryLimit,omitempty"`
	CPULimit    string   `json:"cpuLimit,omitempty"`
	Ports       []string `json:"ports,omitempty"`
	Volumes     []string `json:"volumes,omitempty"`
	Environment []string `json:"environment,omitempty"`
	Networks    []string `json:"networks,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	Detached    bool     `json:"detached,omitempty"`
	Interactive bool     `json:"interactive,omitempty"`
	TTY         bool     `json:"tty,omitempty"`
	Remove      bool     `json:"remove,omitempty"`
	Privileged  bool     `json:"privileged,omitempty"`
}

type ConvertDockerRunRequest struct {
	DockerRunCommand string `json:"dockerRunCommand" binding:"required"`
}

type ConvertDockerRunResponse struct {
	DockerCompose string `json:"dockerCompose"`
	EnvVars       string `json:"envVars"`
	ServiceName   string `json:"serviceName"`
	Success       bool   `json:"success"`
}

// HealthResponse contains the health status of the API.
type HealthResponse struct {
	// Status indicates the health status (e.g., "UP", "DOWN").
	//
	// Required: true
	Status string `json:"status"`
}

// ReadinessResponse is the body of the readiness probe; Checks maps each dependency to "ok" or its error.
type ReadinessResponse struct {
	// Checks maps each dependency name to "ok" or its error.
	//
	// Required: true
	Checks map[string]string `json:"checks"`
	// Status is "ready" or "not_ready".
	//
	// Required: true
	Status string `json:"status"`
}

type PruneContainerMode string

const (
	PruneContainerModeNone      PruneContainerMode = "none"
	PruneContainerModeStopped   PruneContainerMode = "stopped"
	PruneContainerModeOlderThan PruneContainerMode = "olderThan"

	PruneImageModeNone      PruneImageMode = "none"
	PruneImageModeDangling  PruneImageMode = "dangling"
	PruneImageModeAll       PruneImageMode = "all"
	PruneImageModeOlderThan PruneImageMode = "olderThan"

	PruneVolumeModeNone      PruneVolumeMode = "none"
	PruneVolumeModeAnonymous PruneVolumeMode = "anonymous"
	PruneVolumeModeAll       PruneVolumeMode = "all"

	PruneNetworkModeNone      PruneNetworkMode = "none"
	PruneNetworkModeUnused    PruneNetworkMode = "unused"
	PruneNetworkModeOlderThan PruneNetworkMode = "olderThan"

	PruneBuildCacheModeNone      PruneBuildCacheMode = "none"
	PruneBuildCacheModeUnused    PruneBuildCacheMode = "unused"
	PruneBuildCacheModeAll       PruneBuildCacheMode = "all"
	PruneBuildCacheModeOlderThan PruneBuildCacheMode = "olderThan"
)

type PruneImageMode string

type PruneVolumeMode string

type PruneNetworkMode string

type PruneBuildCacheMode string

type PruneContainersOptions struct {
	Mode  PruneContainerMode `json:"mode"`
	Until string             `json:"until,omitempty"`
}

type PruneImagesOptions struct {
	Mode  PruneImageMode `json:"mode"`
	Until string         `json:"until,omitempty"`
}

type PruneVolumesOptions struct {
	Mode PruneVolumeMode `json:"mode"`
}

type PruneNetworksOptions struct {
	Mode  PruneNetworkMode `json:"mode"`
	Until string           `json:"until,omitempty"`
}

type PruneBuildCacheOptions struct {
	Mode  PruneBuildCacheMode `json:"mode"`
	Until string              `json:"until,omitempty"`
}

// PruneAllRequest is used to request pruning of Docker system resources.
type PruneAllRequest struct {
	Containers *PruneContainersOptions `json:"containers,omitempty"`
	Images     *PruneImagesOptions     `json:"images,omitempty"`
	Volumes    *PruneVolumesOptions    `json:"volumes,omitempty"`
	Networks   *PruneNetworksOptions   `json:"networks,omitempty"`
	BuildCache *PruneBuildCacheOptions `json:"buildCache,omitempty"`
}

type SystemDiskUsage struct {
	UnusedImagesBytes      int64 `json:"unusedImagesBytes" doc:"Bytes held by images not referenced by any container"`
	BuildCacheBytes        int64 `json:"buildCacheBytes" doc:"Bytes held by build cache records that are not in use"`
	UnusedVolumesBytes     int64 `json:"unusedVolumesBytes" doc:"Bytes held by volumes no container references"`
	StoppedContainersBytes int64 `json:"stoppedContainersBytes" doc:"Writable-layer bytes held by containers that are not running"`
}

type pruneAllRequestWire struct {
	Dangling   *bool              `json:"dangling,omitempty"`
	Containers stdjson.RawMessage `json:"containers,omitempty"`
	Images     stdjson.RawMessage `json:"images,omitempty"`
	Volumes    stdjson.RawMessage `json:"volumes,omitempty"`
	Networks   stdjson.RawMessage `json:"networks,omitempty"`
	BuildCache stdjson.RawMessage `json:"buildCache,omitempty"`
}

func (r *PruneAllRequest) UnmarshalJSON(data []byte) error {
	var wire pruneAllRequestWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	*r = PruneAllRequest{}

	containers, err := decodePruneContainersOptions(wire.Containers)
	if err != nil {
		return err
	}
	r.Containers = containers

	images, err := decodePruneImagesOptions(wire.Images, wire.Dangling)
	if err != nil {
		return err
	}
	r.Images = images

	volumes, err := decodePruneVolumesOptions(wire.Volumes, wire.Dangling)
	if err != nil {
		return err
	}
	r.Volumes = volumes

	networks, err := decodePruneNetworksOptions(wire.Networks)
	if err != nil {
		return err
	}
	r.Networks = networks

	buildCache, err := decodePruneBuildCacheOptions(wire.BuildCache, wire.Dangling)
	if err != nil {
		return err
	}
	r.BuildCache = buildCache

	return nil
}

func decodePruneContainersOptions(raw stdjson.RawMessage) (*PruneContainersOptions, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if !enabled {
			return nil, nil
		}
		return &PruneContainersOptions{Mode: PruneContainerModeStopped}, nil
	}

	var options PruneContainersOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	return &options, nil
}

func decodePruneImagesOptions(raw stdjson.RawMessage, dangling *bool) (*PruneImagesOptions, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if !enabled {
			return nil, nil
		}
		mode := PruneImageModeDangling
		if dangling != nil && !*dangling {
			mode = PruneImageModeAll
		}
		return &PruneImagesOptions{Mode: mode}, nil
	}

	var options PruneImagesOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	return &options, nil
}

func decodePruneVolumesOptions(raw stdjson.RawMessage, dangling *bool) (*PruneVolumesOptions, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if !enabled {
			return nil, nil
		}
		mode := PruneVolumeModeAnonymous
		if dangling != nil && !*dangling {
			mode = PruneVolumeModeAll
		}
		return &PruneVolumesOptions{Mode: mode}, nil
	}

	var options PruneVolumesOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	return &options, nil
}

func decodePruneNetworksOptions(raw stdjson.RawMessage) (*PruneNetworksOptions, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if !enabled {
			return nil, nil
		}
		return &PruneNetworksOptions{Mode: PruneNetworkModeUnused}, nil
	}

	var options PruneNetworksOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	return &options, nil
}

func decodePruneBuildCacheOptions(raw stdjson.RawMessage, dangling *bool) (*PruneBuildCacheOptions, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if !enabled {
			return nil, nil
		}
		mode := PruneBuildCacheModeUnused
		if dangling != nil && !*dangling {
			mode = PruneBuildCacheModeAll
		}
		return &PruneBuildCacheOptions{Mode: mode}, nil
	}

	var options PruneBuildCacheOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	return &options, nil
}

// PruneAllResult is the result of a prune operation on Docker system resources.
type PruneAllResult struct {
	// ActivityID is the background activity that tracked this prune operation.
	//
	// Required: false
	ActivityID *string `json:"activityId,omitempty"`
	// ContainersPruned is a list of container IDs that were pruned.
	//
	// Required: false
	ContainersPruned []string `json:"containersPruned,omitempty"`

	// ImagesDeleted is a list of image IDs that were deleted.
	//
	// Required: false
	ImagesDeleted []string `json:"imagesDeleted,omitempty"`

	// VolumesDeleted is a list of volume IDs that were deleted.
	//
	// Required: false
	VolumesDeleted []string `json:"volumesDeleted,omitempty"`

	// NetworksDeleted is a list of network IDs that were deleted.
	//
	// Required: false
	NetworksDeleted []string `json:"networksDeleted,omitempty"`

	// Errors is a list of any errors encountered during the prune operation.
	//
	// Required: false
	Errors []string `json:"errors,omitempty"`

	// SpaceReclaimed is the amount of space reclaimed in bytes.
	//
	// Required: true
	SpaceReclaimed uint64 `json:"spaceReclaimed"`

	// ContainerSpaceReclaimed is the amount of space reclaimed from containers in bytes.
	//
	// Required: false
	ContainerSpaceReclaimed uint64 `json:"containerSpaceReclaimed,omitempty"`

	// ImageSpaceReclaimed is the amount of space reclaimed from images in bytes.
	//
	// Required: false
	ImageSpaceReclaimed uint64 `json:"imageSpaceReclaimed,omitempty"`

	// VolumeSpaceReclaimed is the amount of space reclaimed from volumes in bytes.
	//
	// Required: false
	VolumeSpaceReclaimed uint64 `json:"volumeSpaceReclaimed,omitempty"`

	// BuildCacheSpaceReclaimed is the amount of space reclaimed from build cache in bytes.
	//
	// Required: false
	BuildCacheSpaceReclaimed uint64 `json:"buildCacheSpaceReclaimed,omitempty"`

	// Success indicates if the prune operation was successful.
	//
	// Required: true
	Success bool `json:"success"`
}

// GPUStats represents resource statistics for a single GPU.
type GPUStats struct {
	// Name is the GPU model or identifier.
	//
	// Required: true
	Name string `json:"name"`
	// Index is the zero-based GPU index.
	//
	// Required: true
	Index int `json:"index"`
	// MemoryUsed is the GPU memory currently used, in bytes.
	//
	// Required: true
	MemoryUsed float64 `json:"memoryUsed"`
	// MemoryTotal is the total GPU memory available, in bytes.
	//
	// Required: true
	MemoryTotal float64 `json:"memoryTotal"`
}

// SystemStats represents system resource statistics for WebSocket streaming.
type SystemStats struct {
	// Architecture is the CPU architecture (e.g., amd64).
	//
	// Required: true
	Architecture string `json:"architecture"`
	// Platform is the operating system platform (e.g., linux).
	//
	// Required: true
	Platform string `json:"platform"`
	// Hostname is the system hostname.
	Hostname string `json:"hostname,omitempty"`
	// GPUs contains per-GPU resource statistics.
	GPUs []GPUStats `json:"gpus,omitempty"`
	// CPUUsage is the total CPU usage percentage.
	//
	// Required: true
	CPUUsage float64 `json:"cpuUsage"`
	// MemoryUsage is the used system memory, in bytes.
	//
	// Required: true
	MemoryUsage uint64 `json:"memoryUsage"`
	// MemoryTotal is the total system memory, in bytes.
	//
	// Required: true
	MemoryTotal uint64 `json:"memoryTotal"`
	// DiskUsage is the used disk space, in bytes.
	DiskUsage uint64 `json:"diskUsage,omitempty"`
	// DiskTotal is the total disk space, in bytes.
	DiskTotal uint64 `json:"diskTotal,omitempty"`
	// CPUCount is the number of CPUs available to the system.
	//
	// Required: true
	CPUCount int `json:"cpuCount"`
	// GPUCount is the number of GPUs detected.
	//
	// Required: true
	GPUCount int `json:"gpuCount"`
}

// UpgradeCheckResult reports whether the system can be upgraded.
type UpgradeCheckResult struct {
	Message    string `json:"message"`
	CanUpgrade bool   `json:"canUpgrade"`
	Error      bool   `json:"error"`
}

type UpgradeJobResult struct {
	StageStartedAt  *time.Time `json:"stageStartedAt,omitempty"`
	EnvironmentID   string     `json:"environmentId"`
	EnvironmentName string     `json:"environmentName"`
	Status          string     `json:"status"`
	FromVersion     string     `json:"fromVersion,omitempty"`
	FromDigest      string     `json:"fromDigest,omitempty"`
	ToVersion       string     `json:"toVersion,omitempty"`
	Stage           string     `json:"stage,omitempty"`
	Error           string     `json:"error,omitempty"`
	Logs            []string   `json:"logs,omitempty"`
	Percent         int        `json:"percent"`
}

type UpgradeJob struct {
	CreatedAt             time.Time          `json:"createdAt" sortable:"true"`
	UpdatedAt             *time.Time         `json:"updatedAt,omitempty"`
	Error                 *string            `json:"error,omitempty"`
	CompletedAt           *time.Time         `json:"completedAt,omitempty"`
	ID                    string             `json:"id"`
	Status                string             `json:"status"`
	UserID                string             `json:"userId"`
	Username              string             `json:"username"`
	ManagerVersionAtStart string             `json:"managerVersionAtStart"`
	ManagerDigestAtStart  string             `json:"managerDigestAtStart"`
	ManagerTargetVersion  string             `json:"managerTargetVersion"`
	Results               []UpgradeJobResult `json:"results,omitempty"`
}

type StartUpgradeJobRequest struct {
	EnvironmentIDs []string `json:"environmentIds,omitempty" doc:"Environments to upgrade; empty upgrades every enabled environment"`
}

const (
	UpgradeProgressEventStage    = "stage"
	UpgradeProgressEventProgress = "progress"
	UpgradeProgressEventLog      = "log"
	UpgradeProgressEventDone     = "done"
)

type UpgradeProgressEvent struct {
	At          time.Time `json:"at"`
	Type        string    `json:"type" enum:"stage,progress,log,done"`
	Message     string    `json:"message,omitempty"`
	FromVersion string    `json:"fromVersion,omitempty"`
	ToVersion   string    `json:"toVersion,omitempty"`
	Percent     int       `json:"percent"`
	Replay      bool      `json:"replay,omitempty"`
}

// WebSocket connection kind constants.
const (
	WSKindProjectLogs    = "project_logs"
	WSKindContainerLogs  = "container_logs"
	WSKindContainerStats = "container_stats"
	WSKindContainerExec  = "container_exec"
	WSKindSystemStats    = "system_stats"
	WSKindServiceLogs    = "service_logs"
	WSKindDiagnostics    = "diagnostics"
)

// WebSocketConnectionInfo describes a single active WebSocket connection.
type WebSocketConnectionInfo struct {
	// StartedAt is when the connection was established.
	//
	// Required: true
	StartedAt time.Time `json:"startedAt"`
	// ID is the unique identifier for the connection.
	//
	// Required: true
	ID string `json:"id"`
	// Kind is the type of WebSocket connection (e.g. project_logs, container_stats).
	//
	// Required: true
	Kind string `json:"kind"`
	// EnvID is the environment the connection belongs to.
	EnvID string `json:"envId,omitempty"`
	// ResourceID is the ID of the resource being streamed (container, project, etc.).
	ResourceID string `json:"resourceId,omitempty"`
	// ClientIP is the remote address of the client.
	ClientIP string `json:"clientIp,omitempty"`
	// UserID is the authenticated user who opened the connection.
	UserID string `json:"userId,omitempty"`
	// UserAgent is the HTTP User-Agent header from the client.
	UserAgent string `json:"userAgent,omitempty"`
}

// WebSocketMetricsSnapshot is a point-in-time copy of active WebSocket
// connection counts, broken down by kind.
type WebSocketMetricsSnapshot struct {
	// ProjectLogsActive is the number of active project-log streams.
	ProjectLogsActive int64 `json:"projectLogsActive"`
	// ContainerLogsActive is the number of active container-log streams.
	ContainerLogsActive int64 `json:"containerLogsActive"`
	// ContainerStats is the number of active container-stats streams.
	ContainerStats int64 `json:"containerStats"`
	// ContainerExec is the number of active container-exec sessions.
	ContainerExec int64 `json:"containerExec"`
	// SystemStats is the number of active system-stats streams.
	SystemStats int64 `json:"systemStats"`
	// DiagnosticsActive is the number of active diagnostics streams.
	DiagnosticsActive int64 `json:"diagnosticsActive"`
	// ServiceLogsActive is the number of active swarm service-log streams.
	ServiceLogsActive int64 `json:"serviceLogsActive"`
}
