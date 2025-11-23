package utils

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/elastic/gosigar/cgroup"
	"github.com/shirou/gopsutil/v4/host"
)

var (
	// Cache for cgroup limits
	limitsCache      *CgroupLimits
	limitsCacheTime  time.Time
	limitsCacheMutex sync.RWMutex
	limitsCacheTTL   = 30 * time.Second
	limitsCached     bool

	// Cache for container ID
	containerIDCache string
	containerIDOnce  sync.Once
)

type CgroupLimits struct {
	MemoryLimit int64
	MemoryUsage int64
	CPUQuota    int64
	CPUPeriod   int64
	CPUCount    int
}

// DetectCgroupLimits detects cgroup resource limits for the current process.
// It returns memory and CPU limits if the process is running in a restricted cgroup environment.
// Returns error if not in a cgroup or if limits cannot be detected.
func DetectCgroupLimits() (*CgroupLimits, error) {
	// Check cache first
	limitsCacheMutex.RLock()
	if limitsCached && time.Since(limitsCacheTime) < limitsCacheTTL {
		defer limitsCacheMutex.RUnlock()
		if limitsCache == nil {
			return nil, fmt.Errorf("not running in a cgroup")
		}
		return limitsCache, nil
	}
	limitsCacheMutex.RUnlock()

	// Check if we are in a cgroup environment
	if !isInCgroup() {
		// Cache the nil result to avoid repeated checks
		limitsCacheMutex.Lock()
		limitsCache = nil
		limitsCached = true
		limitsCacheTime = time.Now()
		limitsCacheMutex.Unlock()

		// Only log this once per cache cycle (effectively once every 30s)
		slog.Debug("Not running in a cgroup environment")
		return nil, fmt.Errorf("not running in a cgroup")
	}

	slog.Debug("Refreshing cgroup limits")

	reader, err := cgroup.NewReader("/", false)
	if err != nil {
		slog.Error("Failed to create cgroup reader", "error", err)
		return nil, fmt.Errorf("failed to create cgroup reader: %w", err)
	}

	pid := os.Getpid()
	stats, err := reader.GetStatsForProcess(pid)
	if err != nil {
		slog.Error("Failed to get cgroup stats", "error", err)

		// Cache the failure to prevent log spam
		limitsCacheMutex.Lock()
		limitsCache = nil
		limitsCached = true
		limitsCacheTime = time.Now()
		limitsCacheMutex.Unlock()

		return nil, fmt.Errorf("failed to get cgroup stats: %w", err)
	}

	limits := &CgroupLimits{
		MemoryLimit: -1,
		MemoryUsage: -1,
		CPUQuota:    -1,
		CPUPeriod:   -1,
		CPUCount:    -1,
	}

	if stats != nil {
		// Memory limits
		if stats.Memory != nil {
			if stats.Memory.Mem.Limit > 0 && stats.Memory.Mem.Limit < (1<<63-1) {
				limits.MemoryLimit = int64(stats.Memory.Mem.Limit)
			}
			if stats.Memory.Mem.Usage > 0 {
				limits.MemoryUsage = int64(stats.Memory.Mem.Usage)
			}
		}

		// CPU limits
		if stats.CPU != nil && stats.CPU.CFS.QuotaMicros > 0 {
			limits.CPUQuota = int64(stats.CPU.CFS.QuotaMicros)
			limits.CPUPeriod = int64(stats.CPU.CFS.PeriodMicros)

			if limits.CPUQuota > 0 && limits.CPUPeriod > 0 {
				limits.CPUCount = int((limits.CPUQuota + limits.CPUPeriod - 1) / limits.CPUPeriod)
				if limits.CPUCount < 1 {
					limits.CPUCount = 1
				}
			}
		}
	} else {
		slog.Debug("Cgroup stats are nil, assuming no limits")
	}

	slog.Debug("Detected cgroup limits",
		"memory_limit", limits.MemoryLimit,
		"cpu_count", limits.CPUCount)

	// Update cache
	limitsCacheMutex.Lock()
	limitsCache = limits
	limitsCached = true
	limitsCacheTime = time.Now()
	limitsCacheMutex.Unlock()

	return limits, nil
}

// GetContainerID attempts to detect the container ID from cgroup files
func GetContainerID() (string, error) {
	var err error
	containerIDOnce.Do(func() {
		containerIDCache, err = detectContainerID()
	})

	if err != nil {
		return "", err
	}

	if containerIDCache == "" {
		return "", fmt.Errorf("container ID not found")
	}

	return containerIDCache, nil
}

func detectContainerID() (string, error) {
	pid := os.Getpid()
	slog.Debug("Attempting to detect container ID", "pid", pid)

	// Use gosigar to get cgroup paths for the current process
	paths, err := cgroup.ProcessCgroupPaths("/", pid)
	if err != nil {
		slog.Error("Failed to get cgroup paths", "error", err)
		return "", fmt.Errorf("failed to get cgroup paths: %w", err)
	}

	// Iterate over paths to find container ID
	for subsystem, path := range paths {
		slog.Debug("Checking cgroup path", "subsystem", subsystem, "path", path)
		if id := extractContainerIDFromPath(path); id != "" {
			slog.Debug("Found container ID", "id", id, "source_path", path)
			return id, nil
		}
	}

	slog.Warn("Container ID not found in any cgroup path")
	return "", fmt.Errorf("container ID not found in cgroup paths")
}

// Regex patterns for container ID extraction
var (
	// docker-<id>.scope (systemd/cgroup v2)
	systemdScopeRegex = regexp.MustCompile(`docker-([a-f0-9]{12,64})\.scope`)
	// /docker/<id> (cgroup v1)
	dockerPathRegex = regexp.MustCompile(`/docker/([a-f0-9]{12,64})`)
	// Generic 64-char hex string
	genericHexRegex = regexp.MustCompile(`([a-f0-9]{64})`)
)

// extractContainerIDFromPath extracts container ID from a cgroup path
func extractContainerIDFromPath(path string) string {
	// Check for docker-<id>.scope (systemd/cgroup v2)
	if matches := systemdScopeRegex.FindStringSubmatch(path); len(matches) > 1 {
		return matches[1][:12]
	}

	// Check for /docker/<id> (cgroup v1)
	if matches := dockerPathRegex.FindStringSubmatch(path); len(matches) > 1 {
		return matches[1][:12]
	}

	// Generic fallback: look for 64-char hex string
	// This covers cases like /kubepods/.../<id>
	if matches := genericHexRegex.FindStringSubmatch(path); len(matches) > 1 {
		return matches[1][:12]
	}

	return ""
}

// isInCgroup checks if the current process is running in a cgroup environment.
// It checks for virtualization indicators, Docker-specific files, and explicit limits.
func isInCgroup() bool {
	// Check for standard virtualization indicators
	if info, err := host.Info(); err == nil {
		if info.VirtualizationSystem != "" && strings.EqualFold(info.VirtualizationRole, "guest") {
			slog.Debug("Detected virtualization guest role")
			return true
		}
	}

	// Check for Docker specific file
	if _, err := os.Stat("/.dockerenv"); err == nil {
		slog.Debug("Detected .dockerenv file")
		return true
	}

	// Check if we can create a cgroup reader (implies cgroups are available)
	if _, err := cgroup.NewReader("/", false); err == nil {
		slog.Debug("Successfully created cgroup reader")
		return true
	}

	return false
}
