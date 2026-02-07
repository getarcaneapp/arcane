package system

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
	// GPUCount is the number of GPUs detected.
	//
	// Required: true
	GPUCount int `json:"gpuCount"`
	// GPUs contains per-GPU resource statistics.
	GPUs []GPUStats `json:"gpus,omitempty"`
}
