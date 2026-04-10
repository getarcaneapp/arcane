package container

import dockercontainer "github.com/moby/moby/api/types/container"

// StatsHistorySample is a compact history sample for container CPU and memory usage.
type StatsHistorySample struct {
	CPUTenths        uint16 `json:"cpuTenths"`
	MemoryTenths     uint16 `json:"memoryTenths"`
	MemoryUsageBytes uint64 `json:"memoryUsageBytes"`
}

// StatsStreamPayload is the container stats websocket payload.
type StatsStreamPayload struct {
	dockercontainer.StatsResponse
	StatsHistory         []StatsHistorySample `json:"statsHistory,omitempty"`
	CurrentHistorySample StatsHistorySample   `json:"currentHistorySample"`
}
