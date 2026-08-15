package ws

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v5"
	"github.com/samber/hot"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"

	httputil "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
	"go.getarcane.app/sys/cgroup"
)

const (
	cgroupCacheTTL = 30 * time.Second

	// systemStatsSamplerDefaultInterval matches the stats endpoint's default
	// subscriber interval; the shared sampler never ticks faster than the
	// fastest active subscriber.
	systemStatsSamplerDefaultInterval = 2 * time.Second
)

// ============================================================================
// System WebSocket Endpoints
// ============================================================================

// checkRateLimitInternal checks and applies rate limiting for WebSocket connections.
// Returns the counter and whether the connection should be allowed.
func (h *WebSocketHandler) checkRateLimitInternal(clientIP string) (*int32, bool) {
	connCount, _ := h.activeConnections.LoadOrStore(clientIP, new(int32))
	count, ok := connCount.(*int32)
	if !ok {
		return nil, false
	}

	currentCount := atomic.AddInt32(count, 1)
	if currentCount > 5 {
		atomic.AddInt32(count, -1)
		return nil, false
	}
	return count, true
}

// releaseRateLimitInternal decrements the connection counter and cleans up if needed.
func (h *WebSocketHandler) releaseRateLimitInternal(clientIP string, count *int32) {
	newCount := atomic.AddInt32(count, -1)
	if newCount <= 0 {
		// CompareAndDelete, not Delete: a plain delete removed whatever counter
		// was stored for this IP, which after a racing reconnect is a live one —
		// wiping its count and letting that IP exceed the limit.
		h.activeConnections.CompareAndDelete(clientIP, count)
	}
}

func (h *WebSocketHandler) acquireSystemStatsSamplerInternal(ctx context.Context, interval time.Duration) bool {
	h.systemStatsSampler.lifecycleMu.Lock()

	h.systemStatsSampler.clients++
	if h.systemStatsSampler.intervals == nil {
		h.systemStatsSampler.intervals = map[time.Duration]int{}
	}
	h.systemStatsSampler.intervals[interval]++
	h.refreshSystemStatsSamplerIntervalLockedInternal()
	if h.systemStatsSampler.running {
		ready := h.systemStatsSampler.ready
		h.systemStatsSampler.lifecycleMu.Unlock()
		return waitForSystemStatsSamplerReadyInternal(ctx, ready)
	}

	samplerCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	ready := make(chan struct{})
	wake := make(chan struct{}, 1)
	h.systemStatsSampler.cancel = cancel
	h.systemStatsSampler.ready = ready
	h.systemStatsSampler.wake = wake
	h.systemStatsSampler.running = true
	h.systemStatsSampler.lifecycleMu.Unlock()

	collect := h.systemStatsCollector
	if collect == nil {
		collect = h.collectSystemStats
	}

	go func() {
		closeReady := sync.OnceFunc(func() {
			close(ready)
		})
		if !h.initializeCPUCacheCtx(samplerCtx) {
			closeReady()
			return
		}
		if samplerCtx.Err() != nil {
			closeReady()
			return
		}

		h.systemStatsSampler.latest.Store(collect(samplerCtx))
		closeReady()
		if samplerCtx.Err() != nil {
			return
		}

		h.runSystemStatsSamplerInternal(samplerCtx, wake, collect)
	}()

	return waitForSystemStatsSamplerReadyInternal(ctx, ready)
}

func waitForSystemStatsSamplerReadyInternal(ctx context.Context, ready <-chan struct{}) bool {
	if ready == nil {
		return true
	}

	select {
	case <-ctx.Done():
		return false
	case <-ready:
		return ctx.Err() == nil
	}
}

func (h *WebSocketHandler) releaseSystemStatsSamplerInternal(interval time.Duration) {
	var cancel context.CancelFunc

	h.systemStatsSampler.lifecycleMu.Lock()
	if h.systemStatsSampler.clients > 0 {
		h.systemStatsSampler.clients--
	}
	if remaining := h.systemStatsSampler.intervals[interval]; remaining > 1 {
		h.systemStatsSampler.intervals[interval] = remaining - 1
	} else {
		delete(h.systemStatsSampler.intervals, interval)
	}
	h.refreshSystemStatsSamplerIntervalLockedInternal()
	if h.systemStatsSampler.clients == 0 && h.systemStatsSampler.running {
		cancel = h.systemStatsSampler.cancel
		h.systemStatsSampler.cancel = nil
		h.systemStatsSampler.ready = nil
		h.systemStatsSampler.wake = nil
		h.systemStatsSampler.running = false
	}
	h.systemStatsSampler.lifecycleMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// refreshSystemStatsSamplerIntervalLockedInternal recomputes the sampler's
// effective interval — the smallest one any subscriber requested — and must
// be called with lifecycleMu held.
func (h *WebSocketHandler) refreshSystemStatsSamplerIntervalLockedInternal() {
	effective := time.Duration(0)
	for interval := range h.systemStatsSampler.intervals {
		if effective == 0 || interval < effective {
			effective = interval
		}
	}
	if effective <= 0 {
		effective = systemStatsSamplerDefaultInterval
	}
	previous := time.Duration(h.systemStatsSampler.effectiveInterval.Swap(int64(effective)))
	if previous != effective && h.systemStatsSampler.wake != nil {
		select {
		case h.systemStatsSampler.wake <- struct{}{}:
		default:
		}
	}
}

func (h *WebSocketHandler) systemStatsSamplerIntervalInternal() time.Duration {
	if interval := time.Duration(h.systemStatsSampler.effectiveInterval.Load()); interval > 0 {
		return interval
	}
	return systemStatsSamplerDefaultInterval
}

func (h *WebSocketHandler) runSystemStatsSamplerInternal(ctx context.Context, wake <-chan struct{}, collect func(context.Context) systemtypes.SystemStats) {
	interval := h.systemStatsSamplerIntervalInternal()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Subscribers come and go while the sampler runs; re-arm the ticker
	// whenever the fastest requested interval changed.
	rearm := func() {
		if next := h.systemStatsSamplerIntervalInternal(); next != interval {
			interval = next
			ticker.Reset(interval)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			rearm()
		case <-ticker.C:
			h.updateCPUCacheInternal(0)
			h.systemStatsSampler.latest.Store(collect(ctx))
			rearm()
		}
	}
}

// collectSystemStats gathers all system statistics.
func (h *WebSocketHandler) collectSystemStats(ctx context.Context) systemtypes.SystemStats {
	cpuUsage, _ := h.cpuCache.Load()

	cpuCount := h.getCPUCount()
	memUsed, memTotal := h.getMemoryInfo()
	cpuCount, memUsed, memTotal = h.applyCgroupLimits(cpuCount, memUsed, memTotal)
	diskUsed, diskTotal := h.getDiskInfo(ctx)
	hostname := h.getHostname()
	gpuStats, gpuCount := h.getGPUInfo(ctx)

	return systemtypes.SystemStats{
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
}

// getCPUCount returns the number of CPUs.
func (h *WebSocketHandler) getCPUCount() int {
	h.initSystemStaticInfoInternal()
	return h.systemStaticInfo.cpuCount
}

func (h *WebSocketHandler) initSystemStaticInfoInternal() {
	h.systemStaticInfo.once.Do(func() {
		cpuCount, err := cpu.Counts(true)
		if err != nil {
			cpuCount = runtime.NumCPU()
		}

		hostInfo, _ := host.Info()
		hostname := ""
		if hostInfo != nil {
			hostname = hostInfo.Hostname
		}

		h.systemStaticInfo.cpuCount = cpuCount
		h.systemStaticInfo.hostname = hostname
	})
}

// getMemoryInfo returns memory usage and total.
func (h *WebSocketHandler) getMemoryInfo() (uint64, uint64) {
	memInfo, _ := mem.VirtualMemory()
	if memInfo == nil {
		return 0, 0
	}
	// gopsutil counts ZFS ARC as used memory (the kernel excludes it from
	// MemAvailable). Treat the reclaimable portion as cache, matching
	// btop/htop, so the dashboard does not over-report usage on ZFS hosts.
	used := memInfo.Used
	if arc := cgroup.ZFSARCReclaimable(); arc > 0 {
		used -= min(used, arc)
	}
	return used, memInfo.Total
}

// applyCgroupLimits applies cgroup limits when running in an LXC (or similar)
// container where the limits represent the real hardware budget.
//
// It is intentionally a no-op inside Docker: Docker's --cpus / --memory flags
// set artificial cgroup constraints that are unrelated to the host totals we
// want to display. gopsutil already reads the correct host values there (via
// the bind-mounted /proc). Applying cgroup limits on top would produce the
// "#2343 regression" where the dashboard shows "512 MB RAM" while the host
// has 32 GB (#1110).
//
// In LXC the situation is the opposite: gopsutil reads the host's /proc
// (which shows the physical machine's RAM/CPU) rather than the slice of
// resources actually allocated to the LXC guest. The cgroup limits ARE the
// correct numbers to show.
func (h *WebSocketHandler) applyCgroupLimits(cpuCount int, memUsed, memTotal uint64) (int, uint64, uint64) {
	if cgroup.IsDockerContainer() {
		return cpuCount, memUsed, memTotal
	}
	cpuCount = applyCPUAffinityLimitInternal(cpuCount)
	cgroupLimits := h.getCachedCgroupLimitsInternal()
	if cgroupLimits == nil {
		return cpuCount, memUsed, memTotal
	}

	if limit := cgroupLimits.MemoryLimit; limit > 0 {
		limitUint := uint64(limit)
		if memTotal == 0 || limitUint < memTotal {
			memTotal = limitUint
			if cgroupLimits.MemoryUsage > 0 {
				memUsed = uint64(cgroupLimits.MemoryUsage)
			}
		}
	}
	if cgroupLimits.CPUCount > 0 && (cpuCount == 0 || cgroupLimits.CPUCount < cpuCount) {
		cpuCount = cgroupLimits.CPUCount
	}
	return cpuCount, memUsed, memTotal
}

// applyCPUAffinityLimitInternal caps the CPU count at the scheduler affinity
// mask size. In an unprivileged LXC guest the cgroup cpu quota is usually
// unset at the guest root (the limit lives host-side), so the affinity mask is
// the only signal of the cores actually assigned — including gap bindings like
// CPU1+CPU7 — while /proc/cpuinfo shows the whole host (#3161). runtime.NumCPU
// reads that mask at process start. Not applied inside Docker, matching
// applyCgroupLimits' host-totals convention there.
func applyCPUAffinityLimitInternal(cpuCount int) int {
	if n := runtime.NumCPU(); n > 0 && (cpuCount == 0 || n < cpuCount) {
		return n
	}
	return cpuCount
}

// getDiskInfo returns disk usage and total.
func (h *WebSocketHandler) getDiskInfo(ctx context.Context) (uint64, uint64) {
	diskUsagePath := h.getDiskUsagePath(ctx)
	diskInfo, err := disk.Usage(diskUsagePath)
	if err != nil || diskInfo == nil || diskInfo.Total == 0 {
		if diskUsagePath != "/" {
			diskInfo, _ = disk.Usage("/")
		}
	}
	if diskInfo == nil {
		return 0, 0
	}
	return diskInfo.Used, diskInfo.Total
}

// getHostname returns the system hostname.
func (h *WebSocketHandler) getHostname() string {
	h.initSystemStaticInfoInternal()
	return h.systemStaticInfo.hostname
}

// getGPUInfo returns GPU statistics if monitoring is enabled.
func (h *WebSocketHandler) getGPUInfo(ctx context.Context) ([]systemtypes.GPUStats, int) {
	if h.gpuMonitor == nil || !h.gpuMonitor.Enabled() {
		return nil, 0
	}
	gpuData, err := h.gpuMonitor.Stats(ctx)
	if err != nil {
		return nil, 0
	}
	return gpuData, len(gpuData)
}

// initializeCPUCacheCtx performs initial CPU sampling and returns early if the sampler is canceled.
func (h *WebSocketHandler) initializeCPUCacheCtx(ctx context.Context) bool {
	result := make(chan float64, 1)

	go func() {
		if val, ok := h.readCPUUsageInternal(time.Second); ok {
			result <- val
		}
		close(result)
	}()

	select {
	case <-ctx.Done():
		return false
	case val, ok := <-result:
		if !ok || ctx.Err() != nil {
			return false
		}
		h.cpuCache.Store(val)
		return true
	}
}

func (h *WebSocketHandler) updateCPUCacheInternal(interval time.Duration) {
	if val, ok := h.readCPUUsageInternal(interval); ok {
		h.cpuCache.Store(val)
	}
}

func (h *WebSocketHandler) readCPUUsageInternal(interval time.Duration) (float64, bool) {
	if h.cpuUsageReader != nil {
		return h.cpuUsageReader(interval)
	}

	return defaultReadCPUUsageInternal(interval)
}

func defaultReadCPUUsageInternal(interval time.Duration) (float64, bool) {
	if vals, err := cpu.Percent(interval, false); err == nil && len(vals) > 0 {
		return vals[0], true
	}

	return 0, false
}

func (h *WebSocketHandler) getCachedCgroupLimitsInternal() *cgroup.Limits {
	if h.cgroupCache == nil {
		return nil
	}
	return h.cgroupCache.Get()
}

// SystemStats streams system stats over WebSocket.
//
//	@Summary		Get system stats via WebSocket
//	@Description	Stream system resource statistics over WebSocket connection
//	@Tags			WebSocket
//	@Param			id	path	string	true	"Environment ID"
//	@Router			/api/environments/{id}/ws/system/stats [get]
func (h *WebSocketHandler) SystemStats(c *echo.Context) error {
	clientIP := c.RealIP()

	count, allowed := h.checkRateLimitInternal(clientIP)
	if !allowed {
		return wsErrorJSONInternal(c, http.StatusTooManyRequests, "Too many concurrent stats connections from this IP")
	}
	defer h.releaseRateLimitInternal(clientIP, count)

	conn, unregister, ok := h.acceptWSInternal(c, systemtypes.WSKindSystemStats, "")
	if !ok {
		return nil
	}
	defer unregister()
	defer func() {
		if err := conn.CloseNow(); err != nil {
			slog.Debug("Failed to close system stats websocket connection", "clientIP", clientIP, "error", err)
		}
	}()

	interval, _ := httputil.GetIntQueryParam(c.Request(), "interval", false)
	if interval <= 0 {
		interval = 2
	}
	sampleInterval := time.Duration(interval) * time.Second

	conn.SetReadLimit(512)

	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()
	if !h.acquireSystemStatsSamplerInternal(ctx, sampleInterval) {
		h.releaseSystemStatsSamplerInternal(sampleInterval)
		return nil
	}
	defer h.releaseSystemStatsSamplerInternal(sampleInterval)

	go h.readSystemStatsPumpInternal(ctx, cancel, conn)
	// The pong is serviced by readSystemStatsPumpInternal.
	go keepWSConnAliveInternal(ctx, cancel, conn, 54*time.Second)

	send := func() error {
		stats, _ := h.systemStatsSampler.latest.Load()
		b, err := json.Marshal(stats)
		if err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return conn.Write(wctx, websocket.MessageText, b)
	}

	if err := send(); err != nil {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := send(); err != nil {
				return nil
			}
		}
	}
}

// readSystemStatsPumpInternal is the single reader for the SystemStats websocket.
// Do not add additional readers for this connection.
func (h *WebSocketHandler) readSystemStatsPumpInternal(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn) {
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			cancel()
			return
		}
	}
}

func (h *WebSocketHandler) getDiskUsagePath(ctx context.Context) string {
	if h.diskUsagePathCache == nil {
		h.diskUsagePathCache = hot.NewHotCache[struct{}, string](hot.LRU, 1).
			WithTTL(5 * time.Minute).
			Build()
	}
	path, found, err := h.diskUsagePathCache.GetWithLoaders(struct{}{}, func(_ []struct{}) (map[struct{}]string, error) {
		path := "/"
		if h.systemService != nil {
			path = h.systemService.GetDiskUsagePath(ctx)
		}
		return map[struct{}]string{{}: path}, nil
	})
	if err != nil || !found {
		return "/"
	}
	return path
}
