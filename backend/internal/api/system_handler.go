package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/hertg/gopci/pkg/pci"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/config"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/middleware"
	"github.com/ofkm/arcane-backend/internal/models"
	"github.com/ofkm/arcane-backend/internal/services"
	"github.com/ofkm/arcane-backend/internal/utils"
	httputil "github.com/ofkm/arcane-backend/internal/utils/http"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

const (
	gpuCacheDuration = 30 * time.Second
)

// GPUStats represents statistics for a single GPU
type GPUStats struct {
	Name        string  `json:"name"`
	Index       int     `json:"index"`
	MemoryUsed  float64 `json:"memoryUsed"`  // in bytes
	MemoryTotal float64 `json:"memoryTotal"` // in bytes
}

type SystemHandler struct {
	dockerService     *services.DockerClientService
	systemService     *services.SystemService
	upgradeService    *services.SystemUpgradeService
	sysWsUpgrader     websocket.Upgrader
	activeConnections sync.Map
	cpuCache          struct {
		sync.RWMutex
		value     float64
		timestamp time.Time
	}
	diskUsagePathCache struct {
		sync.RWMutex
		value     string
		timestamp time.Time
	}
	gpuDetectionCache struct {
		sync.RWMutex
		detected  bool
		timestamp time.Time
		gpuType   string // "nvidia", "amd", "intel", "jetson", or ""
		toolPath  string
	}
	detectionDone  bool
	detectionMutex sync.Mutex
}

func NewSystemHandler(group *gin.RouterGroup, dockerService *services.DockerClientService, systemService *services.SystemService, upgradeService *services.SystemUpgradeService, authMiddleware *middleware.AuthMiddleware, cfg *config.Config) {
	handler := &SystemHandler{
		dockerService:  dockerService,
		systemService:  systemService,
		upgradeService: upgradeService,
		sysWsUpgrader: websocket.Upgrader{
			CheckOrigin: httputil.ValidateWebSocketOrigin(cfg.AppUrl),
		},
	}

	apiGroup := group.Group("/environments/:id/system")
	apiGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{
		apiGroup.HEAD("/health", handler.Health)
		apiGroup.GET("/stats/ws", handler.Stats)
		apiGroup.GET("/docker/info", handler.GetDockerInfo)
		apiGroup.POST("/prune", handler.PruneAll)
		apiGroup.POST("/containers/start-all", handler.StartAllContainers)
		apiGroup.POST("/containers/start-stopped", handler.StartAllStoppedContainers)
		apiGroup.POST("/containers/stop-all", handler.StopAllContainers)
		apiGroup.POST("/convert", handler.ConvertDockerRun)

		// Upgrade endpoints (admin required)
		apiGroup.GET("/upgrade/check", authMiddleware.WithAdminRequired().Add(), handler.CheckUpgradeAvailable)
		apiGroup.POST("/upgrade", authMiddleware.WithAdminRequired().Add(), handler.TriggerUpgrade)
	}
}

type SystemStats struct {
	CPUUsage     float64    `json:"cpuUsage"`
	MemoryUsage  uint64     `json:"memoryUsage"`
	MemoryTotal  uint64     `json:"memoryTotal"`
	DiskUsage    uint64     `json:"diskUsage,omitempty"`
	DiskTotal    uint64     `json:"diskTotal,omitempty"`
	CPUCount     int        `json:"cpuCount"`
	Architecture string     `json:"architecture"`
	Platform     string     `json:"platform"`
	Hostname     string     `json:"hostname,omitempty"`
	GPUCount     int        `json:"gpuCount"`
	GPUs         []GPUStats `json:"gpus,omitempty"`
}

func (h *SystemHandler) Health(c *gin.Context) {
	ctx := c.Request.Context()

	dockerClient, err := h.dockerService.CreateConnection(ctx)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	defer dockerClient.Close()

	// Try to ping Docker to ensure it's responsive
	_, err = dockerClient.Ping(ctx)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	c.Status(http.StatusOK)
}

func (h *SystemHandler) GetDockerInfo(c *gin.Context) {
	ctx := c.Request.Context()

	dockerClient, err := h.dockerService.CreateConnection(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.DockerConnectionError{Err: err}).Error(),
		})
		return
	}
	defer dockerClient.Close()

	version, err := dockerClient.ServerVersion(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.DockerVersionError{Err: err}).Error(),
		})
		return
	}

	info, err := dockerClient.Info(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.DockerInfoError{Err: err}).Error(),
		})
		return
	}

	cpuCount := info.NCPU
	memTotal := info.MemTotal

	// Check for cgroup limits (LXC, Docker, etc.)
	if cgroupLimits, err := utils.DetectCgroupLimits(); err == nil {
		// Use cgroup memory limit if available and smaller than host value
		if limit := cgroupLimits.MemoryLimit; limit > 0 {
			limitInt := int64(limit)
			if memTotal == 0 || limitInt < memTotal {
				memTotal = limitInt
			}
		}

		// Use cgroup CPU count if available
		if cgroupLimits.CPUCount > 0 && (cpuCount == 0 || cgroupLimits.CPUCount < cpuCount) {
			cpuCount = cgroupLimits.CPUCount
		}
	}

	// Update info with cgroup limits
	info.NCPU = cpuCount
	info.MemTotal = memTotal

	dockerInfo := dto.DockerInfoDto{
		Success:    true,
		APIVersion: version.APIVersion,
		GitCommit:  version.GitCommit,
		GoVersion:  version.GoVersion,
		Os:         version.Os,
		Arch:       version.Arch,
		BuildTime:  version.BuildTime,
		Info:       info,
	}

	c.JSON(http.StatusOK, dockerInfo)
}

func (h *SystemHandler) PruneAll(c *gin.Context) {
	ctx := c.Request.Context()
	slog.InfoContext(ctx, "System prune operation initiated")

	var req dto.PruneSystemDto
	if err := c.ShouldBindJSON(&req); err != nil {
		slog.ErrorContext(ctx, "Failed to bind prune request JSON",
			slog.String("error", err.Error()),
			slog.String("client_ip", c.ClientIP()))
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   (&common.InvalidRequestFormatError{Err: err}).Error(),
		})
		return
	}

	slog.InfoContext(ctx, "Prune request parsed successfully",
		slog.Bool("containers", req.Containers),
		slog.Bool("images", req.Images),
		slog.Bool("volumes", req.Volumes),
		slog.Bool("networks", req.Networks),
		slog.Bool("build_cache", req.BuildCache),
		slog.Bool("dangling", req.Dangling))

	result, err := h.systemService.PruneAll(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "System prune operation failed",
			slog.String("error", err.Error()),
			slog.String("client_ip", c.ClientIP()))
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.SystemPruneError{Err: err}).Error(),
		})
		return
	}

	slog.InfoContext(ctx, "System prune operation completed successfully",
		slog.Int("containers_pruned", len(result.ContainersPruned)),
		slog.Int("images_deleted", len(result.ImagesDeleted)),
		slog.Int("volumes_deleted", len(result.VolumesDeleted)),
		slog.Int("networks_deleted", len(result.NetworksDeleted)),
		slog.Uint64("space_reclaimed", result.SpaceReclaimed),
		slog.Bool("success", result.Success),
		slog.Int("error_count", len(result.Errors)))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Pruning completed",
		"data":    result,
	})
}

func (h *SystemHandler) StartAllContainers(c *gin.Context) {
	result, err := h.systemService.StartAllContainers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.ContainerStartAllError{Err: err}).Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Container start operation completed",
		"data":    result,
	})
}

func (h *SystemHandler) StartAllStoppedContainers(c *gin.Context) {
	result, err := h.systemService.StartAllStoppedContainers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.ContainerStartStoppedError{Err: err}).Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Stopped containers start operation completed",
		"data":    result,
	})
}

func (h *SystemHandler) getDiskUsagePath(ctx context.Context) string {
	h.diskUsagePathCache.RLock()
	if h.diskUsagePathCache.value != "" && time.Since(h.diskUsagePathCache.timestamp) < 30*time.Second {
		path := h.diskUsagePathCache.value
		h.diskUsagePathCache.RUnlock()
		return path
	}
	h.diskUsagePathCache.RUnlock()

	diskUsagePath := h.systemService.GetDiskUsagePath(ctx)
	if diskUsagePath == "" {
		diskUsagePath = "/"
	}

	h.diskUsagePathCache.Lock()
	h.diskUsagePathCache.value = diskUsagePath
	h.diskUsagePathCache.timestamp = time.Now()
	h.diskUsagePathCache.Unlock()

	return diskUsagePath
}

func (h *SystemHandler) StopAllContainers(c *gin.Context) {
	result, err := h.systemService.StopAllContainers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.ContainerStopAllError{Err: err}).Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Container stop operation completed",
		"data":    result,
	})
}

//nolint:gocognit
func (h *SystemHandler) Stats(c *gin.Context) {
	clientIP := c.ClientIP()

	connCount, _ := h.activeConnections.LoadOrStore(clientIP, new(int32))
	count := connCount.(*int32)

	currentCount := atomic.AddInt32(count, 1)
	if currentCount > 5 {
		atomic.AddInt32(count, -1)
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"error":   "Too many concurrent stats connections from this IP",
		})
		return
	}

	defer func() {
		newCount := atomic.AddInt32(count, -1)
		if newCount <= 0 {
			h.activeConnections.Delete(clientIP)
		}
	}()

	conn, err := h.sysWsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	cpuUpdateTicker := time.NewTicker(1 * time.Second)
	defer cpuUpdateTicker.Stop()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-cpuUpdateTicker.C:
				if vals, err := cpu.Percent(0, false); err == nil && len(vals) > 0 {
					h.cpuCache.Lock()
					h.cpuCache.value = vals[0]
					h.cpuCache.timestamp = time.Now()
					h.cpuCache.Unlock()
				}
			}
		}
	}(ctx)

	send := func() error {
		h.cpuCache.RLock()
		cpuUsage := h.cpuCache.value
		h.cpuCache.RUnlock()

		cpuCount, err := cpu.Counts(true)
		if err != nil {
			cpuCount = runtime.NumCPU()
		}

		memInfo, _ := mem.VirtualMemory()
		var memUsed, memTotal uint64
		if memInfo != nil {
			memUsed = memInfo.Used
			memTotal = memInfo.Total
		}

		if cgroupLimits, err := utils.DetectCgroupLimits(); err == nil {
			// Use cgroup memory limits if available and smaller than host values
			if limit := cgroupLimits.MemoryLimit; limit > 0 {
				limitUint := uint64(limit)
				if memTotal == 0 || limitUint < memTotal {
					memTotal = limitUint
				}
			}

			// Use actual cgroup memory usage if available
			if usage := cgroupLimits.MemoryUsage; usage > 0 {
				memUsed = uint64(usage)
			}

			// Use cgroup CPU count if available
			if cgroupLimits.CPUCount > 0 && (cpuCount == 0 || cgroupLimits.CPUCount < cpuCount) {
				cpuCount = cgroupLimits.CPUCount
			}
		}

		diskUsagePath := h.getDiskUsagePath(ctx)
		diskInfo, err := disk.Usage(diskUsagePath)
		if err != nil || diskInfo == nil || diskInfo.Total == 0 {
			if diskUsagePath != "/" {
				diskInfo, _ = disk.Usage("/")
			}
		}

		var diskUsed, diskTotal uint64
		if diskInfo != nil {
			diskUsed = diskInfo.Used
			diskTotal = diskInfo.Total
		}

		hostInfo, _ := host.Info()
		var hostname string
		if hostInfo != nil {
			hostname = hostInfo.Hostname
		}

		// Collect GPU stats (non-blocking, fails gracefully)
		var gpuStats []GPUStats
		var gpuCount int
		if gpuData, err := h.getGPUStats(ctx); err == nil {
			gpuStats = gpuData
			gpuCount = len(gpuData)
		}

		stats := SystemStats{
			CPUUsage:     cpuUsage,
			MemoryUsage:  memUsed,
			MemoryTotal:  memTotal,
			DiskUsage:    diskUsed,
			DiskTotal:    diskTotal,
			CPUCount:     cpuCount,
			Architecture: runtime.GOARCH,
			Platform:     runtime.GOOS,
			Hostname:     hostname,
			GPUCount:     gpuCount,
			GPUs:         gpuStats,
		}

		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(stats)
	}

	if vals, err := cpu.Percent(time.Second, false); err == nil && len(vals) > 0 {
		h.cpuCache.Lock()
		h.cpuCache.value = vals[0]
		h.cpuCache.timestamp = time.Now()
		h.cpuCache.Unlock()
	}

	if err := send(); err != nil {
		return
	}

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if err := send(); err != nil {
				return
			}
		}
	}
}

func (h *SystemHandler) ConvertDockerRun(c *gin.Context) {
	var req models.ConvertDockerRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   (&common.InvalidRequestFormatError{Err: err}).Error(),
		})
		return
	}

	parsed, err := h.systemService.ParseDockerRunCommand(req.DockerRunCommand)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   (&common.DockerRunParseError{Err: err}).Error(),
			"code":    "BAD_REQUEST",
		})
		return
	}

	dockerCompose, envVars, serviceName, err := h.systemService.ConvertToDockerCompose(parsed)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.DockerComposeConversionError{Err: err}).Error(),
			"code":    "CONVERSION_ERROR",
		})
		return
	}

	c.JSON(http.StatusOK, models.ConvertDockerRunResponse{
		Success:       true,
		DockerCompose: dockerCompose,
		EnvVars:       envVars,
		ServiceName:   serviceName,
	})
}

// CheckUpgradeAvailable checks if the local system can be upgraded
// Remote environments are handled by the proxy middleware
func (h *SystemHandler) CheckUpgradeAvailable(c *gin.Context) {
	canUpgrade, err := h.upgradeService.CanUpgrade(c.Request.Context())

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"canUpgrade": false,
			"error":      true,
			"message":    (&common.UpgradeCheckError{Err: err}).Error(),
		})
		slog.Debug("System upgrade check failed", "error", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"canUpgrade": canUpgrade,
		"error":      false,
		"message":    "System can be upgraded",
	})
}

// TriggerUpgrade triggers a system upgrade by spawning the upgrade CLI command
// This runs the upgrade from outside the current container to avoid self-termination issues
func (h *SystemHandler) TriggerUpgrade(c *gin.Context) {
	currentUser, ok := middleware.RequireAuthentication(c)
	if !ok {
		return
	}

	slog.Info("System upgrade triggered", "user", currentUser.Username, "userId", currentUser.ID)

	err := h.upgradeService.TriggerUpgradeViaCLI(c.Request.Context(), *currentUser)
	if err != nil {
		slog.Error("System upgrade failed", "error", err, "user", currentUser.Username)

		statusCode := http.StatusInternalServerError
		if errors.Is(err, services.ErrUpgradeInProgress) {
			statusCode = http.StatusConflict
		}

		c.JSON(statusCode, gin.H{
			"error":   (&common.UpgradeTriggerError{Err: err}).Error(),
			"message": "Failed to initiate upgrade",
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Upgrade initiated successfully. A new container is being created and will replace this one shortly.",
		"success": true,
	})
}

// getGPUStats collects and returns GPU statistics for all available GPUs
func (h *SystemHandler) getGPUStats(ctx context.Context) ([]GPUStats, error) {
	// Check if we need to detect GPUs
	h.detectionMutex.Lock()
	done := h.detectionDone
	h.detectionMutex.Unlock()

	if !done {
		if err := h.detectGPUs(ctx); err != nil {
			return nil, err
		}
	}

	// Check cache
	h.gpuDetectionCache.RLock()
	if h.gpuDetectionCache.detected && time.Since(h.gpuDetectionCache.timestamp) < gpuCacheDuration {
		gpuType := h.gpuDetectionCache.gpuType
		h.gpuDetectionCache.RUnlock()

		// Collect stats based on GPU type
		switch gpuType {
		case "nvidia":
			return h.getNvidiaStats(ctx)
		case "amd":
			return h.getAMDStats(ctx)
		case "intel":
			return h.getIntelStats(ctx)
		case "jetson":
			return h.getJetsonStats(ctx)
		}
	}
	h.gpuDetectionCache.RUnlock()

	// Re-detect if cache expired
	if err := h.detectGPUs(ctx); err != nil {
		return nil, err
	}

	// Try again after detection
	h.gpuDetectionCache.RLock()
	gpuType := h.gpuDetectionCache.gpuType
	h.gpuDetectionCache.RUnlock()

	switch gpuType {
	case "nvidia":
		return h.getNvidiaStats(ctx)
	case "amd":
		return h.getAMDStats(ctx)
	case "intel":
		return h.getIntelStats(ctx)
	case "jetson":
		return h.getJetsonStats(ctx)
	default:
		return nil, fmt.Errorf("no supported GPU found")
	}
}

// detectGPUs detects available GPU management tools
func (h *SystemHandler) detectGPUs(ctx context.Context) error {
	h.detectionMutex.Lock()
	defer h.detectionMutex.Unlock()

	// Check for NVIDIA
	if path, err := exec.LookPath("nvidia-smi"); err == nil {
		h.gpuDetectionCache.Lock()
		h.gpuDetectionCache.detected = true
		h.gpuDetectionCache.gpuType = "nvidia"
		h.gpuDetectionCache.toolPath = path
		h.gpuDetectionCache.timestamp = time.Now()
		h.gpuDetectionCache.Unlock()
		h.detectionDone = true
		slog.InfoContext(ctx, "NVIDIA GPU detected", slog.String("tool", "nvidia-smi"), slog.String("path", path))
		return nil
	}

	// Check for AMD ROCm
	if path, err := exec.LookPath("rocm-smi"); err == nil {
		h.gpuDetectionCache.Lock()
		h.gpuDetectionCache.detected = true
		h.gpuDetectionCache.gpuType = "amd"
		h.gpuDetectionCache.toolPath = path
		h.gpuDetectionCache.timestamp = time.Now()
		h.gpuDetectionCache.Unlock()
		h.detectionDone = true
		slog.InfoContext(ctx, "AMD GPU detected", slog.String("tool", "rocm-smi"), slog.String("path", path))
		return nil
	}

	// Check for NVIDIA Jetson
	if path, err := exec.LookPath("tegrastats"); err == nil {
		h.gpuDetectionCache.Lock()
		h.gpuDetectionCache.detected = true
		h.gpuDetectionCache.gpuType = "jetson"
		h.gpuDetectionCache.toolPath = path
		h.gpuDetectionCache.timestamp = time.Now()
		h.gpuDetectionCache.Unlock()
		h.detectionDone = true
		slog.InfoContext(ctx, "NVIDIA Jetson detected", slog.String("tool", "tegrastats"), slog.String("path", path))
		return nil
	}

	// Check for Intel GPU
	if path, err := exec.LookPath("intel_gpu_top"); err == nil {
		h.gpuDetectionCache.Lock()
		h.gpuDetectionCache.detected = true
		h.gpuDetectionCache.gpuType = "intel"
		h.gpuDetectionCache.toolPath = path
		h.gpuDetectionCache.timestamp = time.Now()
		h.gpuDetectionCache.Unlock()
		h.detectionDone = true
		slog.InfoContext(ctx, "Intel GPU detected", slog.String("tool", "intel_gpu_top"), slog.String("path", path))
		return nil
	}

	h.detectionDone = true
	return fmt.Errorf("no supported GPU found")
}

// getNvidiaStats collects NVIDIA GPU statistics using nvidia-smi
func (h *SystemHandler) getNvidiaStats(ctx context.Context) ([]GPUStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Query: index, name, memory.used, memory.total
	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=index,name,memory.used,memory.total",
		"--format=csv,noheader,nounits")

	output, err := cmd.Output()
	if err != nil {
		slog.WarnContext(ctx, "Failed to execute nvidia-smi", slog.String("error", err.Error()))
		return nil, fmt.Errorf("nvidia-smi execution failed: %w", err)
	}

	return h.parseNvidiaOutput(ctx, output)
}

// parseNvidiaOutput parses CSV output from nvidia-smi
func (h *SystemHandler) parseNvidiaOutput(ctx context.Context, output []byte) ([]GPUStats, error) {
	reader := csv.NewReader(bytes.NewReader(output))
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse nvidia-smi CSV output", slog.String("error", err.Error()))
		return nil, fmt.Errorf("failed to parse nvidia-smi output: %w", err)
	}

	var stats []GPUStats
	for _, record := range records {
		if len(record) < 4 {
			continue
		}

		index, err := strconv.Atoi(strings.TrimSpace(record[0]))
		if err != nil {
			slog.WarnContext(ctx, "Failed to parse GPU index", slog.String("value", record[0]))
			continue
		}

		name := strings.TrimSpace(record[1])

		memUsed, err := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
		if err != nil {
			slog.WarnContext(ctx, "Failed to parse memory used", slog.String("value", record[2]))
			continue
		}

		memTotal, err := strconv.ParseFloat(strings.TrimSpace(record[3]), 64)
		if err != nil {
			slog.WarnContext(ctx, "Failed to parse memory total", slog.String("value", record[3]))
			continue
		}

		// nvidia-smi returns MiB (mebibytes), convert to bytes (1 MiB = 1024*1024 bytes)
		stats = append(stats, GPUStats{
			Name:        name,
			Index:       index,
			MemoryUsed:  memUsed * 1024 * 1024,
			MemoryTotal: memTotal * 1024 * 1024,
		})
	}

	if len(stats) == 0 {
		return nil, fmt.Errorf("no GPU data parsed from nvidia-smi")
	}

	slog.DebugContext(ctx, "Collected NVIDIA GPU stats", slog.Int("gpu_count", len(stats)))
	return stats, nil
}

// getAMDStats collects AMD GPU statistics using rocm-smi
func (h *SystemHandler) getAMDStats(ctx context.Context) ([]GPUStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "rocm-smi", "--showmeminfo", "vram", "--json")
	output, err := cmd.Output()
	if err != nil {
		slog.WarnContext(ctx, "Failed to execute rocm-smi", slog.String("error", err.Error()))
		return nil, fmt.Errorf("rocm-smi execution failed: %w", err)
	}

	return h.parseROCmOutput(ctx, output)
}

// ROCmSMIOutput represents the JSON structure from rocm-smi
type ROCmSMIOutput map[string]ROCmGPUInfo

type ROCmGPUInfo struct {
	VRAMUsed  string `json:"VRAM Total Used Memory (B)"`
	VRAMTotal string `json:"VRAM Total Memory (B)"`
}

// parseROCmOutput parses JSON output from rocm-smi
func (h *SystemHandler) parseROCmOutput(ctx context.Context, output []byte) ([]GPUStats, error) {
	var rocmData ROCmSMIOutput
	if err := json.Unmarshal(output, &rocmData); err != nil {
		slog.WarnContext(ctx, "Failed to parse rocm-smi JSON output", slog.String("error", err.Error()))
		return nil, fmt.Errorf("failed to parse rocm-smi output: %w", err)
	}

	var stats []GPUStats
	index := 0
	for gpuID, info := range rocmData {
		// Parse memory used (in bytes)
		memUsedBytes, err := strconv.ParseFloat(info.VRAMUsed, 64)
		if err != nil {
			slog.WarnContext(ctx, "Failed to parse AMD memory used", slog.String("gpu", gpuID), slog.String("value", info.VRAMUsed))
			continue
		}

		// Parse memory total (in bytes)
		memTotalBytes, err := strconv.ParseFloat(info.VRAMTotal, 64)
		if err != nil {
			slog.WarnContext(ctx, "Failed to parse AMD memory total", slog.String("gpu", gpuID), slog.String("value", info.VRAMTotal))
			continue
		}

		// ROCm already returns bytes, use directly
		stats = append(stats, GPUStats{
			Name:        fmt.Sprintf("AMD GPU %s", gpuID),
			Index:       index,
			MemoryUsed:  memUsedBytes,
			MemoryTotal: memTotalBytes,
		})
		index++
	}

	if len(stats) == 0 {
		return nil, fmt.Errorf("no GPU data parsed from rocm-smi")
	}

	slog.DebugContext(ctx, "Collected AMD GPU stats", slog.Int("gpu_count", len(stats)))
	return stats, nil
}

// getIntelStats collects Intel GPU statistics using gopci library
func (h *SystemHandler) getIntelStats(ctx context.Context) ([]GPUStats, error) {
	// Scan for VGA-compatible devices (class 0x03)
	gpuClassFilter := func(d *pci.Device) bool {
		return d.Class.Class() == 0x03 && d.Vendor.ID == 0x8086 // Intel vendor ID
	}

	devices, err := pci.Scan(gpuClassFilter)
	if err != nil {
		slog.WarnContext(ctx, "Failed to scan PCI devices",
			slog.String("error", err.Error()))
		return []GPUStats{{Name: "Intel GPU", Index: 0}}, nil
	}

	if len(devices) == 0 {
		slog.DebugContext(ctx, "No Intel GPU devices found via PCI scan")
		return []GPUStats{{Name: "Intel GPU", Index: 0}}, nil
	}

	var stats []GPUStats
	for i, device := range devices {
		gpuName := fmt.Sprintf("Intel %s", device.Product.Label)
		if strings.Contains(gpuName, "Device ") {
			gpuName = fmt.Sprintf("Intel GPU (0x%04x)", device.Product.ID)
		}

		// Try to read VRAM from sysfs using device address
		// Note: Intel integrated GPUs use system RAM and typically don't expose
		// mem_info_vram_* files. Only discrete GPUs (like Intel Arc) have these.
		var memUsed, memTotal float64
		sysfsPath := device.SysfsPath()

		// Check for discrete GPU VRAM info
		if totalData, err := os.ReadFile(filepath.Join(sysfsPath, "mem_info_vram_total")); err == nil {
			memTotal, _ = strconv.ParseFloat(strings.TrimSpace(string(totalData)), 64)
			if usedData, err := os.ReadFile(filepath.Join(sysfsPath, "mem_info_vram_used")); err == nil {
				memUsed, _ = strconv.ParseFloat(strings.TrimSpace(string(usedData)), 64)
			}
		} else {
			// For integrated GPUs, try reading from i915 gem_objects if available
			// This requires debugfs access which may not be available in containers
			i915Path := "/sys/kernel/debug/dri/0/i915_gem_objects"
			if data, err := os.ReadFile(i915Path); err == nil {
				// Parse i915_gem_objects output for memory usage
				// Format contains lines like: "123456 objects, 234567890 bytes"
				lines := strings.Split(string(data), "\n")
				for _, line := range lines {
					if strings.Contains(line, "bytes") && strings.Contains(line, "objects") {
						fields := strings.Fields(line)
						if len(fields) >= 3 {
							if bytes, err := strconv.ParseFloat(fields[2], 64); err == nil {
								memUsed = bytes
								break
							}
						}
					}
				}
			}
		}

		stats = append(stats, GPUStats{
			Name:        gpuName,
			Index:       i,
			MemoryUsed:  memUsed,
			MemoryTotal: memTotal,
		})

		slog.DebugContext(ctx, "Collected Intel GPU stats via gopci",
			slog.String("name", gpuName),
			slog.String("address", device.Address.Hex()),
			slog.Float64("memory_used_bytes", memUsed),
			slog.Float64("memory_total_bytes", memTotal))
	}

	return stats, nil
}

func readIntelVRAMInfo() (float64, float64, error) {
	matches, err := filepath.Glob("/sys/class/drm/card*/device/mem_info_vram_total")
	if err != nil {
		return 0, 0, fmt.Errorf("glob mem_info_vram_total: %w", err)
	}
	for _, totalPath := range matches {
		total, err := readFloatFromFile(totalPath)
		if err != nil {
			continue
		}
		usedPath := strings.Replace(totalPath, "mem_info_vram_total", "mem_info_vram_used", 1)
		used, err := readFloatFromFile(usedPath)
		if err != nil {
			// Some drivers may not expose used metrics
			used = 0
		}
		return used, total, nil
	}
	return 0, 0, fmt.Errorf("mem_info_vram_total not found")
}

func readFloatFromFile(path string) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return 0, err
	}
	return value, nil
}

// IntelGPUTopOutput represents the JSON structure from intel_gpu_top
type IntelGPUTopOutput struct {
	Period struct {
		Duration float64 `json:"duration"`
		Unit     string  `json:"unit"`
	} `json:"period"`
	Frequency struct {
		Requested float64 `json:"requested"`
		Actual    float64 `json:"actual"`
		Unit      string  `json:"unit"`
	} `json:"frequency"`
	Interrupts struct {
		Count float64 `json:"count"`
		Unit  string  `json:"unit"`
	} `json:"interrupts"`
	RC6 struct {
		Value float64 `json:"value"`
		Unit  string  `json:"unit"`
	} `json:"rc6"`
	Power struct {
		GPU     float64 `json:"GPU"`
		Package float64 `json:"Package"`
		Unit    string  `json:"unit"`
	} `json:"power"`
	IMCBandwidth struct {
		Reads  float64 `json:"reads"`
		Writes float64 `json:"writes"`
		Unit   string  `json:"unit"`
	} `json:"imc-bandwidth"`
	Engines map[string]struct {
		Busy float64 `json:"busy"`
		Sema float64 `json:"sema"`
		Wait float64 `json:"wait"`
		Unit string  `json:"unit"`
	} `json:"engines"`
}

// parseIntelOutput parses JSON output from intel_gpu_top
func (h *SystemHandler) parseIntelOutput(ctx context.Context, output []byte) ([]GPUStats, error) {
	var intelData IntelGPUTopOutput
	if err := json.Unmarshal(output, &intelData); err != nil {
		slog.WarnContext(ctx, "Failed to parse intel_gpu_top JSON",
			slog.String("error", err.Error()))
		return nil, fmt.Errorf("failed to parse intel_gpu_top output: %w", err)
	}

	// Get VRAM info from sysfs
	usedBytes, totalBytes, err := readIntelVRAMInfo()
	if err != nil {
		slog.DebugContext(ctx, "VRAM info not available from sysfs",
			slog.String("error", err.Error()))
		usedBytes = 0
		totalBytes = 0
	}

	slog.DebugContext(ctx, "Collected Intel GPU stats",
		slog.Float64("frequency_mhz", intelData.Frequency.Actual),
		slog.Float64("power_gpu_w", intelData.Power.GPU),
		slog.Float64("power_package_w", intelData.Power.Package),
		slog.Float64("rc6_percent", intelData.RC6.Value),
		slog.Float64("memory_used_bytes", usedBytes),
		slog.Float64("memory_total_bytes", totalBytes))

	stats := []GPUStats{
		{
			Name:        "Intel GPU",
			Index:       0,
			MemoryUsed:  usedBytes,
			MemoryTotal: totalBytes,
		},
	}

	return stats, nil
}

// getJetsonStats collects NVIDIA Jetson statistics using tegrastats
func (h *SystemHandler) getJetsonStats(ctx context.Context) ([]GPUStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// tegrastats outputs continuous stream, we'll use --interval for single sample
	cmd := exec.CommandContext(ctx, "tegrastats", "--interval", "1000")
	output, err := cmd.Output()
	if err != nil {
		slog.WarnContext(ctx, "Failed to execute tegrastats", slog.String("error", err.Error()))
		return nil, fmt.Errorf("tegrastats execution failed: %w", err)
	}

	return h.parseJetsonOutput(ctx, output)
}

// parseJetsonOutput parses text output from tegrastats
func (h *SystemHandler) parseJetsonOutput(ctx context.Context, output []byte) ([]GPUStats, error) {
	// tegrastats output format: RAM 1234/5678MB (lfb 1x2MB) ...
	// This is a simplified parser for MVP

	lines := strings.Split(string(output), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("no output from tegrastats")
	}

	// Parse first line for RAM info (Jetson uses unified memory)
	// Example: "RAM 1234/5678MB"
	for _, line := range lines {
		if strings.Contains(line, "RAM") {
			// Simple parsing - extract RAM usage
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "RAM" && i+1 < len(parts) {
					memPart := parts[i+1]
					if strings.Contains(memPart, "/") && strings.HasSuffix(memPart, "MB") {
						memPart = strings.TrimSuffix(memPart, "MB")
						memValues := strings.Split(memPart, "/")
						if len(memValues) == 2 {
							used, _ := strconv.ParseFloat(memValues[0], 64)
							total, _ := strconv.ParseFloat(memValues[1], 64)

							// tegrastats returns MB, convert to bytes
							return []GPUStats{
								{
									Name:        "NVIDIA Jetson",
									Index:       0,
									MemoryUsed:  used * 1024 * 1024,
									MemoryTotal: total * 1024 * 1024,
								},
							}, nil
						}
					}
				}
			}
		}
	}

	slog.DebugContext(ctx, "NVIDIA Jetson detected but could not parse memory stats")
	return nil, fmt.Errorf("failed to parse tegrastats output")
}
