package types

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"time"
)

type (
	Diagnostics struct {
		Timestamp time.Time            `json:"timestamp"`
		GC        GCInfo               `json:"gc"`
		Runtime   RuntimeInfo          `json:"runtime"`
		WebSocket WebSocketDiagnostics `json:"websocket"`
		Memory    MemoryInfo           `json:"memory"`
	}

	RuntimeInfo struct {
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

	GoroutineLeakReport struct {
		ScannedAt time.Time `json:"scannedAt"`
		Profile   string    `json:"profile"`
		Count     int       `json:"count"`
	}

	GoroutineProfile struct {
		CapturedAt time.Time        `json:"capturedAt"`
		Groups     []GoroutineGroup `json:"groups"`
		Total      int              `json:"total"`
	}

	GoroutineGroup struct {
		Label  string           `json:"label"`
		Frames []GoroutineFrame `json:"frames"`
		Count  int              `json:"count"`
	}

	GoroutineFrame struct {
		Function string `json:"function"`
		File     string `json:"file"`
		Line     int    `json:"line"`
	}

	MemoryInfo struct {
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

	GCInfo struct {
		LastGC         time.Time `json:"lastGc"`
		RecentPausesNs []int64   `json:"recentPausesNs"`
		NumGC          int64     `json:"numGc"`
		PauseTotalNs   int64     `json:"pauseTotalNs"`
	}

	WebSocketDiagnostics struct {
		Connections []WebSocketConnectionInfo `json:"connections"`
		Snapshot    WebSocketMetricsSnapshot  `json:"snapshot"`
	}

	ActorDiagnostics struct {
		CheckedAt       time.Time       `json:"checkedAt"`
		Runs            map[string]int  `json:"runs"`
		EnvironmentID   string          `json:"environmentId"`
		EnvironmentName string          `json:"environmentName"`
		Error           string          `json:"error,omitempty"`
		Hosts           []ActorHostInfo `json:"hosts"`
		ActorTypes      []ActorTypeInfo `json:"actorTypes"`
		Ready           bool            `json:"ready"`
	}

	ActorHostInfo struct {
		LastHealthCheck time.Time `json:"lastHealthCheck"`
		HostID          string    `json:"hostId"`
		Address         string    `json:"address"`
	}

	ActorTypeInfo struct {
		NextAlarmAt      time.Time         `json:"nextAlarmAt,omitzero"`
		ActorType        string            `json:"actorType"`
		Instances        []ActorInstance   `json:"instances"`
		RecentFailures   []ActorJobFailure `json:"recentFailures"`
		ConcurrencyLimit int               `json:"concurrencyLimit"`
		ActiveActors     int               `json:"activeActors"`
		StoredStates     int               `json:"storedStates"`
		PendingAlarms    int               `json:"pendingAlarms"`
		PendingJobs      int               `json:"pendingJobs"`
		LeasedAlarms     int               `json:"leasedAlarms"`
		CompletedJobs    int               `json:"completedJobs"`
		DeadJobs         int               `json:"deadJobs"`
	}

	ActorInstance struct {
		ActivatedAt time.Time `json:"activatedAt"`
		ActorID     string    `json:"actorId"`
		Name        string    `json:"name,omitempty"`
		HostID      string    `json:"hostId"`
	}

	ActorJobFailure struct {
		FailedAt  time.Time `json:"failedAt"`
		JobID     string    `json:"jobId"`
		ActorID   string    `json:"actorId"`
		Name      string    `json:"name,omitempty"`
		Method    string    `json:"method"`
		LastError string    `json:"lastError,omitempty"`
		Attempts  int       `json:"attempts"`
	}

	DiagnosticsCommand struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Name    string `json:"name,omitempty"`
		Seconds int    `json:"seconds,omitempty"`
	}

	DiagnosticsMessage struct {
		Snapshot   *Diagnostics         `json:"snapshot,omitempty"`
		LeakReport *GoroutineLeakReport `json:"leakReport,omitempty"`
		Type       string               `json:"type"`
		ID         string               `json:"id,omitempty"`
		Text       string               `json:"text,omitempty"`
		Error      string               `json:"error,omitempty"`
		Data       []byte               `json:"data,omitempty"`
	}

	DockerRunCommand struct {
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

	ConvertDockerRunRequest struct {
		DockerRunCommand string `json:"dockerRunCommand" binding:"required"`
	}

	ConvertDockerRunResponse struct {
		DockerCompose string `json:"dockerCompose"`
		EnvVars       string `json:"envVars"`
		ServiceName   string `json:"serviceName"`
		Success       bool   `json:"success"`
	}

	HealthResponse struct {
		Status string `json:"status"`
	}

	ReadinessResponse struct {
		Checks map[string]string `json:"checks"`
		Status string            `json:"status"`
	}

	PruneContainerMode string

	PruneImageMode string

	PruneVolumeMode string

	PruneNetworkMode string

	PruneBuildCacheMode string

	PruneContainersOptions struct {
		Mode  PruneContainerMode `json:"mode"`
		Until string             `json:"until,omitempty"`
	}

	PruneImagesOptions struct {
		Mode  PruneImageMode `json:"mode"`
		Until string         `json:"until,omitempty"`
	}

	PruneVolumesOptions struct {
		Mode PruneVolumeMode `json:"mode"`
	}

	PruneNetworksOptions struct {
		Mode  PruneNetworkMode `json:"mode"`
		Until string           `json:"until,omitempty"`
	}

	PruneBuildCacheOptions struct {
		Mode  PruneBuildCacheMode `json:"mode"`
		Until string              `json:"until,omitempty"`
	}

	PruneAllRequest struct {
		Containers *PruneContainersOptions `json:"containers,omitempty"`
		Images     *PruneImagesOptions     `json:"images,omitempty"`
		Volumes    *PruneVolumesOptions    `json:"volumes,omitempty"`
		Networks   *PruneNetworksOptions   `json:"networks,omitempty"`
		BuildCache *PruneBuildCacheOptions `json:"buildCache,omitempty"`
	}

	SystemDiskUsage struct {
		UnusedImagesBytes      int64 `json:"unusedImagesBytes" doc:"Bytes held by images not referenced by any container"`
		BuildCacheBytes        int64 `json:"buildCacheBytes" doc:"Bytes held by build cache records that are not in use"`
		UnusedVolumesBytes     int64 `json:"unusedVolumesBytes" doc:"Bytes held by volumes no container references"`
		StoppedContainersBytes int64 `json:"stoppedContainersBytes" doc:"Writable-layer bytes held by containers that are not running"`
	}

	pruneAllRequestWire struct {
		Dangling   *bool          `json:"dangling,omitempty"`
		Containers jsontext.Value `json:"containers,omitempty"`
		Images     jsontext.Value `json:"images,omitempty"`
		Volumes    jsontext.Value `json:"volumes,omitempty"`
		Networks   jsontext.Value `json:"networks,omitempty"`
		BuildCache jsontext.Value `json:"buildCache,omitempty"`
	}

	PruneAllResult struct {
		ActivityID       *string  `json:"activityId,omitempty"`
		ContainersPruned []string `json:"containersPruned,omitempty"`
		ImagesDeleted    []string `json:"imagesDeleted,omitempty"`
		VolumesDeleted   []string `json:"volumesDeleted,omitempty"`
		NetworksDeleted  []string `json:"networksDeleted,omitempty"`
		Errors           []string `json:"errors,omitempty"`

		SpaceReclaimed uint64 `json:"spaceReclaimed"`

		ContainerSpaceReclaimed uint64 `json:"containerSpaceReclaimed,omitempty"`

		ImageSpaceReclaimed uint64 `json:"imageSpaceReclaimed,omitempty"`

		VolumeSpaceReclaimed uint64 `json:"volumeSpaceReclaimed,omitempty"`

		BuildCacheSpaceReclaimed uint64 `json:"buildCacheSpaceReclaimed,omitempty"`
		Success                  bool   `json:"success"`
	}

	GPUStats struct {
		Name        string  `json:"name"`
		Index       int     `json:"index"`
		MemoryUsed  float64 `json:"memoryUsed"`
		MemoryTotal float64 `json:"memoryTotal"`
	}

	SystemStats struct {
		Architecture string     `json:"architecture"`
		Platform     string     `json:"platform"`
		Hostname     string     `json:"hostname,omitempty"`
		GPUs         []GPUStats `json:"gpus,omitempty"`
		CPUUsage     float64    `json:"cpuUsage"`
		MemoryUsage  uint64     `json:"memoryUsage"`
		MemoryTotal  uint64     `json:"memoryTotal"`
		DiskUsage    uint64     `json:"diskUsage,omitempty"`
		DiskTotal    uint64     `json:"diskTotal,omitempty"`
		CPUCount     int        `json:"cpuCount"`
		GPUCount     int        `json:"gpuCount"`
	}

	UpgradeCheckResult struct {
		Message    string `json:"message"`
		CanUpgrade bool   `json:"canUpgrade"`
		Error      bool   `json:"error"`
	}

	UpgradeJobResult struct {
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

	UpgradeJob struct {
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

	StartUpgradeJobRequest struct {
		EnvironmentIDs []string `json:"environmentIds,omitempty" doc:"Environments to upgrade; empty upgrades every enabled environment"`
	}

	UpgradeProgressEvent struct {
		At          time.Time `json:"at"`
		Type        string    `json:"type" enum:"stage,progress,log,done"`
		Message     string    `json:"message,omitempty"`
		FromVersion string    `json:"fromVersion,omitempty"`
		ToVersion   string    `json:"toVersion,omitempty"`
		Percent     int       `json:"percent"`
		Replay      bool      `json:"replay,omitempty"`
	}

	WebSocketConnectionInfo struct {
		StartedAt  time.Time `json:"startedAt"`
		ID         string    `json:"id"`
		Kind       string    `json:"kind"`
		EnvID      string    `json:"envId,omitempty"`
		ResourceID string    `json:"resourceId,omitempty"`
		ClientIP   string    `json:"clientIp,omitempty"`
		UserID     string    `json:"userId,omitempty"`
		UserAgent  string    `json:"userAgent,omitempty"`
	}

	WebSocketMetricsSnapshot struct {
		ProjectLogsActive   int64 `json:"projectLogsActive"`
		ContainerLogsActive int64 `json:"containerLogsActive"`
		ContainerStats      int64 `json:"containerStats"`
		ContainerExec       int64 `json:"containerExec"`
		SystemStats         int64 `json:"systemStats"`
		DiagnosticsActive   int64 `json:"diagnosticsActive"`
		ServiceLogsActive   int64 `json:"serviceLogsActive"`
	}
)

const (
	PruneContainerModeNone       PruneContainerMode  = "none"
	PruneContainerModeStopped    PruneContainerMode  = "stopped"
	PruneContainerModeOlderThan  PruneContainerMode  = "olderThan"
	PruneImageModeNone           PruneImageMode      = "none"
	PruneImageModeDangling       PruneImageMode      = "dangling"
	PruneImageModeAll            PruneImageMode      = "all"
	PruneImageModeOlderThan      PruneImageMode      = "olderThan"
	PruneVolumeModeNone          PruneVolumeMode     = "none"
	PruneVolumeModeAnonymous     PruneVolumeMode     = "anonymous"
	PruneVolumeModeAll           PruneVolumeMode     = "all"
	PruneNetworkModeNone         PruneNetworkMode    = "none"
	PruneNetworkModeUnused       PruneNetworkMode    = "unused"
	PruneNetworkModeOlderThan    PruneNetworkMode    = "olderThan"
	PruneBuildCacheModeNone      PruneBuildCacheMode = "none"
	PruneBuildCacheModeUnused    PruneBuildCacheMode = "unused"
	PruneBuildCacheModeAll       PruneBuildCacheMode = "all"
	PruneBuildCacheModeOlderThan PruneBuildCacheMode = "olderThan"

	UpgradeProgressEventStage    = "stage"
	UpgradeProgressEventProgress = "progress"
	UpgradeProgressEventLog      = "log"
	UpgradeProgressEventDone     = "done"

	WSKindProjectLogs    = "project_logs"
	WSKindContainerLogs  = "container_logs"
	WSKindContainerStats = "container_stats"
	WSKindContainerExec  = "container_exec"
	WSKindSystemStats    = "system_stats"
	WSKindServiceLogs    = "service_logs"
	WSKindDiagnostics    = "diagnostics"
)

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

func decodePruneContainersOptions(raw jsontext.Value) (*PruneContainersOptions, error) {
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

func decodePruneImagesOptions(raw jsontext.Value, dangling *bool) (*PruneImagesOptions, error) {
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

func decodePruneVolumesOptions(raw jsontext.Value, dangling *bool) (*PruneVolumesOptions, error) {
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

func decodePruneNetworksOptions(raw jsontext.Value) (*PruneNetworksOptions, error) {
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

func decodePruneBuildCacheOptions(raw jsontext.Value, dangling *bool) (*PruneBuildCacheOptions, error) {
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
