package docker

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/host"
)

const unrestricted = 9223372036854771712

type CgroupLimits struct {
	MemoryLimit int64
	MemoryUsage int64
	CPUQuota    int64
	CPUPeriod   int64
	CPUCount    int
}

func DetectCgroupLimits() (*CgroupLimits, error) {
	limits := &CgroupLimits{
		MemoryLimit: -1,
		MemoryUsage: -1,
		CPUQuota:    -1,
		CPUPeriod:   -1,
		CPUCount:    -1,
	}

	if !isInCgroup() {
		return nil, fmt.Errorf("not running in a cgroup")
	}

	if isCgroupV2() {
		return detectCgroupV2Limits(limits)
	}

	return detectCgroupV1Limits(limits)
}

func isInCgroup() bool {
	if info, err := host.Info(); err == nil {
		if info.VirtualizationSystem != "" && strings.EqualFold(info.VirtualizationRole, "guest") {
			return true
		}
	}

	return hasExplicitCgroupLimit()
}

func isCgroupV2() bool {
	_, err := os.Stat("/sys/fs/cgroup/cgroup.controllers")
	return err == nil
}

func hasExplicitCgroupLimit() bool {
	if isCgroupV2() {
		if limit, err := readCgroupV2Int64("/sys/fs/cgroup/memory.max"); err == nil && isFiniteLimit(limit) {
			return true
		}

		if quota, period := readCgroupV2CPU(); quota > 0 && period > 0 {
			return true
		}

		return false
	}

	cgroupPath, err := getCgroupV1Path()
	if err != nil || cgroupPath == "" {
		return false
	}

	memLimitPath := filepath.Join("/sys/fs/cgroup/memory", cgroupPath, "memory.limit_in_bytes")
	if limit, err := readCgroupV1Int64(memLimitPath); err == nil && isFiniteLimit(limit) {
		return true
	}

	if quota, err := readCgroupV1CPUControllerInt64(cgroupPath, "cpu.cfs_quota_us"); err == nil && quota > 0 {
		if period, err := readCgroupV1CPUControllerInt64(cgroupPath, "cpu.cfs_period_us"); err == nil && period > 0 {
			return true
		}
	}

	return false
}

func detectCgroupV2Limits(limits *CgroupLimits) (*CgroupLimits, error) {
	if memLimit, err := readCgroupV2Int64("/sys/fs/cgroup/memory.max"); err == nil {
		if memLimit != unrestricted {
			limits.MemoryLimit = memLimit
		}
	}

	if memUsage, err := readCgroupV2Int64("/sys/fs/cgroup/memory.current"); err == nil {
		limits.MemoryUsage = memUsage
	}

	if cpuMax, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		parts := strings.Fields(string(cpuMax))
		if len(parts) >= 2 {
			if parts[0] != "max" {
				if quota, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
					limits.CPUQuota = quota
				}
			}
			if period, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				limits.CPUPeriod = period
			}
		}
	}

	if limits.CPUQuota > 0 && limits.CPUPeriod > 0 {
		limits.CPUCount = int((limits.CPUQuota + limits.CPUPeriod - 1) / limits.CPUPeriod)
		if limits.CPUCount < 1 {
			limits.CPUCount = 1
		}
	}

	return limits, nil
}

func detectCgroupV1Limits(limits *CgroupLimits) (*CgroupLimits, error) {
	cgroupPath, err := getCgroupV1Path()
	if err != nil {
		return limits, err
	}

	memoryLimitPath := filepath.Join("/sys/fs/cgroup/memory", cgroupPath, "memory.limit_in_bytes")
	if memLimit, err := readCgroupV1Int64(memoryLimitPath); err == nil {
		if memLimit < unrestricted {
			limits.MemoryLimit = memLimit
		}
	}

	memoryUsagePath := filepath.Join("/sys/fs/cgroup/memory", cgroupPath, "memory.usage_in_bytes")
	if memUsage, err := readCgroupV1Int64(memoryUsagePath); err == nil {
		limits.MemoryUsage = memUsage
	}

	if cpuQuota, err := readCgroupV1CPUControllerInt64(cgroupPath, "cpu.cfs_quota_us"); err == nil {
		limits.CPUQuota = cpuQuota
	}

	if cpuPeriod, err := readCgroupV1CPUControllerInt64(cgroupPath, "cpu.cfs_period_us"); err == nil {
		limits.CPUPeriod = cpuPeriod
	}

	if limits.CPUQuota > 0 && limits.CPUPeriod > 0 {
		limits.CPUCount = int((limits.CPUQuota + limits.CPUPeriod - 1) / limits.CPUPeriod)
		if limits.CPUCount < 1 {
			limits.CPUCount = 1
		}
	}

	return limits, nil
}

func getCgroupV1Path() (string, error) {
	file, err := os.Open("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 {
			if strings.Contains(parts[1], "memory") || strings.Contains(parts[1], "cpu") {
				return strings.TrimPrefix(parts[2], "/"), nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error scanning cgroup file: %w", err)
	}

	return "", fmt.Errorf("cgroup path not found")
}

func readCgroupV1Int64(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, err
	}

	return value, nil
}

func readCgroupV2Int64(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	content := strings.TrimSpace(string(data))
	if content == "max" {
		return unrestricted, nil
	}

	value, err := strconv.ParseInt(content, 10, 64)
	if err != nil {
		return 0, err
	}

	return value, nil
}

func readCgroupV2CPU() (int64, int64) {
	data, err := os.ReadFile("/sys/fs/cgroup/cpu.max")
	if err != nil {
		return -1, -1
	}

	parts := strings.Fields(string(data))
	if len(parts) < 2 {
		return -1, -1
	}

	var quota int64
	if parts[0] != "max" {
		if q, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
			quota = q
		}
	}

	period, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return -1, -1
	}

	return quota, period
}

func isFiniteLimit(value int64) bool {
	return value > 0 && value < unrestricted
}

func readCgroupV1CPUControllerInt64(cgroupPath, filename string) (int64, error) {
	controllerBases := []string{
		"/sys/fs/cgroup/cpu,cpuacct",
		"/sys/fs/cgroup/cpu",
		"/sys/fs/cgroup/cpuacct",
	}

	var lastErr error
	for _, base := range controllerBases {
		value, err := readCgroupV1Int64(filepath.Join(base, cgroupPath, filename))
		if err == nil {
			return value, nil
		}
		lastErr = err
	}

	return 0, lastErr
}

// Container ID detection patterns for cgroup v1 and v2
var (
	// cgroup v1: "12:memory:/docker/abc123..." or "12:memory:/kubepods/.../docker/abc123..."
	cgroupV1ContainerPattern = regexp.MustCompile(`/docker/([a-f0-9]{64})`)
	// cgroup v2: "0::/system.slice/docker-abc123.scope"
	cgroupV2ContainerPattern = regexp.MustCompile(`docker-([a-f0-9]{64})\.scope`)
)

// GetCurrentContainerID detects the current container ID using multiple detection methods
// It tries cgroup, mountinfo, and hostname in that order
func GetCurrentContainerID() (string, error) {
	// Try cgroup first (works on cgroup v1 and cgroupns=host mode)
	if id, err := getContainerIDFromCgroup(); err == nil {
		slog.Debug("GetCurrentContainerID: found via cgroup", "containerId", id)
		return id, nil
	}

	// Try mountinfo (works when cgroup namespace is private)
	if id, err := getContainerIDFromMountinfo(); err == nil {
		slog.Debug("GetCurrentContainerID: found via mountinfo", "containerId", id)
		return id, nil
	}

	// Try hostname (Docker often sets hostname to container ID)
	if id, err := getContainerIDFromHostname(); err == nil {
		slog.Debug("GetCurrentContainerID: found via hostname", "containerId", id)
		return id, nil
	}

	return "", errors.New("no container ID found via cgroup, mountinfo, or hostname")
}

// getContainerIDFromCgroup tries to extract container ID from /proc/self/cgroup
func getContainerIDFromCgroup() (string, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}

	content := string(data)

	// Try cgroup v1 pattern
	if matches := cgroupV1ContainerPattern.FindStringSubmatch(content); len(matches) >= 2 {
		return matches[1], nil
	}

	// Try cgroup v2 pattern
	if matches := cgroupV2ContainerPattern.FindStringSubmatch(content); len(matches) >= 2 {
		return matches[1], nil
	}

	return "", errors.New("no container ID found in cgroup")
}

// getContainerIDFromMountinfo tries to extract container ID from /proc/self/mountinfo
// This works even when cgroup namespace is private (cgroupns=private)
func getContainerIDFromMountinfo() (string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		// Look for Docker container paths in mountinfo
		if strings.Contains(line, "/docker/containers/") {
			parts := strings.Split(line, "/docker/containers/")
			if len(parts) > 1 {
				idParts := strings.Split(parts[1], "/")
				if len(idParts) > 0 && len(idParts[0]) >= 12 {
					id := idParts[0]
					if len(id) >= 64 {
						return id[:64], nil
					}
					return id, nil
				}
			}
		}
	}

	return "", errors.New("no container ID found in mountinfo")
}

// getContainerIDFromHostname tries to use hostname as container ID
// Docker often sets the hostname to the container ID (12 or 64 hex chars)
func getContainerIDFromHostname() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}

	// Docker often sets hostname to the short container ID (12 chars) or full ID (64 chars)
	if len(hostname) == 12 || len(hostname) == 64 {
		// Verify it looks like a hex string
		for _, c := range hostname {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return "", errors.New("hostname doesn't match container ID pattern")
			}
		}
		return hostname, nil
	}

	return "", errors.New("hostname doesn't match expected container ID length")
}
