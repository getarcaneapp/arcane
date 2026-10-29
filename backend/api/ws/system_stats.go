package ws

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"

	wshub "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/ws"
	httputil "github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
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

func (h *WebSocketHandler) acquireSystemStatsSampler(ctx context.Context, interval time.Duration) bool {
	h.systemStatsSampler.lifecycleMu.Lock()

	h.systemStatsSampler.clients++
	if h.systemStatsSampler.intervals == nil {
		h.systemStatsSampler.intervals = map[time.Duration]int{}
	}
	h.systemStatsSampler.intervals[interval]++
	h.refreshSystemStatsSamplerIntervalLocked()
	if h.systemStatsSampler.running {
		ready := h.systemStatsSampler.ready
		h.systemStatsSampler.lifecycleMu.Unlock()
		return waitForSystemStatsSamplerReady(ctx, ready)
	}

	samplerCtx, cancel := context.WithCancel(h.ctx)
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
		if val, ok := h.readCPUUsage(0); ok {
			h.cpuCache.Store(val)
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

		h.runSystemStatsSampler(samplerCtx, wake, collect)
	}()

	return waitForSystemStatsSamplerReady(ctx, ready)
}

func waitForSystemStatsSamplerReady(ctx context.Context, ready <-chan struct{}) bool {
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

func (h *WebSocketHandler) releaseSystemStatsSampler(interval time.Duration) {
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
	h.refreshSystemStatsSamplerIntervalLocked()
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

// refreshSystemStatsSamplerIntervalLocked recomputes the sampler's
// effective interval — the smallest one any subscriber requested — and must
// be called with lifecycleMu held.
func (h *WebSocketHandler) refreshSystemStatsSamplerIntervalLocked() {
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

func (h *WebSocketHandler) systemStatsSamplerInterval() time.Duration {
	if interval := time.Duration(h.systemStatsSampler.effectiveInterval.Load()); interval > 0 {
		return interval
	}
	return systemStatsSamplerDefaultInterval
}

func (h *WebSocketHandler) runSystemStatsSampler(ctx context.Context, wake <-chan struct{}, collect func(context.Context) types.SystemStats) {
	interval := h.systemStatsSamplerInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Subscribers come and go while the sampler runs; re-arm the ticker
	// whenever the fastest requested interval changed.
	rearm := func() {
		if next := h.systemStatsSamplerInterval(); next != interval {
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
			if val, ok := h.readCPUUsage(0); ok {
				h.cpuCache.Store(val)
			}
			h.systemStatsSampler.latest.Store(collect(ctx))
			rearm()
		}
	}
}

// collectSystemStats gathers all system statistics.
func (h *WebSocketHandler) collectSystemStats(ctx context.Context) types.SystemStats {
	cpuUsage, _ := h.cpuCache.Load()

	h.systemStaticInfo.once.Do(func() {
		cpuCount, err := cpu.Counts(true)
		if err != nil {
			cpuCount = runtime.NumCPU()
		}
		hostInfo, _ := host.Info()
		h.systemStaticInfo.cpuCount = cpuCount
		h.systemStaticInfo.hostname = kit.FromPtr(hostInfo).Hostname
	})

	var memUsed, memTotal uint64
	if memInfo, _ := mem.VirtualMemory(); memInfo != nil {
		// gopsutil counts ZFS ARC as used memory; treat the reclaimable portion as cache, matching btop/htop.
		memUsed = memInfo.Used - min(memInfo.Used, cgroup.ZFSARCReclaimable())
		memTotal = memInfo.Total
	}
	cpuCount, memUsed, memTotal := h.applyCgroupLimits(ctx, h.systemStaticInfo.cpuCount, memUsed, memTotal)

	diskUsagePath, found, err := h.diskUsagePathCache.GetWithLoaders(struct{}{}, func(_ []struct{}) (map[struct{}]string, error) {
		path := "/"
		if h.systemService != nil {
			path = h.systemService.GetDiskUsagePath(ctx)
		}
		return map[struct{}]string{{}: path}, nil
	})
	diskUsagePath = kit.Ternary(err != nil || !found, "/", diskUsagePath)
	diskInfo, err := disk.Usage(diskUsagePath)
	if (err != nil || diskInfo == nil || diskInfo.Total == 0) && diskUsagePath != "/" {
		diskInfo, _ = disk.Usage("/")
	}

	var gpuStats []types.GPUStats
	if monitor := h.gpuMonitor.Load(); monitor != nil && monitor.Enabled() {
		if stats, statsErr := monitor.Stats(ctx); statsErr == nil {
			gpuStats = stats
		}
	}

	return types.SystemStats{
		CPUUsage:     cpuUsage,
		MemoryUsage:  memUsed,
		MemoryTotal:  memTotal,
		DiskUsage:    kit.FromPtr(diskInfo).Used,
		DiskTotal:    kit.FromPtr(diskInfo).Total,
		CPUCount:     cpuCount,
		Architecture: runtime.GOARCH,
		Platform:     runtime.GOOS,
		Hostname:     h.systemStaticInfo.hostname,
		GPUCount:     len(gpuStats),
		GPUs:         gpuStats,
	}
}

// applyCgroupLimits uses guest resources without applying Docker's
// per-container limits to host statistics.
func (h *WebSocketHandler) applyCgroupLimits(ctx context.Context, cpuCount int, memUsed, memTotal uint64) (int, uint64, uint64) {
	if cgroup.IsDockerContainer() {
		if h.systemService != nil {
			if used, total, ok := h.systemService.GetDockerHostMemory(ctx); ok {
				return cpuCount, used, total
			}
		}
		return cpuCount, memUsed, memTotal
	}
	cpuCount = applyCPUAffinityLimit(cpuCount)
	cgroupLimits := h.cgroupCache.Get()
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

// applyCPUAffinityLimit caps the CPU count at the scheduler affinity
// mask size. In an unprivileged LXC guest the cgroup cpu quota is usually
// unset at the guest root (the limit lives host-side), so the affinity mask is
// the only signal of the cores actually assigned — including gap bindings like
// CPU1+CPU7 — while /proc/cpuinfo shows the whole host (#3161). runtime.NumCPU
// reads that mask at process start. Not applied inside Docker, matching
// applyCgroupLimits's host-totals convention there.
func applyCPUAffinityLimit(cpuCount int) int {
	if n := runtime.NumCPU(); n > 0 && (cpuCount == 0 || n < cpuCount) {
		return n
	}
	return cpuCount
}

func (h *WebSocketHandler) readCPUUsage(interval time.Duration) (float64, bool) {
	if h.cpuUsageReader != nil {
		return h.cpuUsageReader(interval)
	}
	if vals, err := cpu.Percent(interval, false); err == nil && len(vals) > 0 {
		return vals[0], true
	}
	return 0, false
}

// SystemStats streams system stats over WebSocket.
//
//	@Summary		Get system stats via WebSocket
//	@Description	Stream system resource statistics over WebSocket connection
//	@Tags			WebSocket
//	@Param			id	path	string	true	"Environment ID"
//	@Router			/api/environments/{id}/ws/system/stats [get]
func (h *WebSocketHandler) SystemStats(c *echo.Context) error {
	userID, _ := c.Get("userID").(string)
	clientKey := cmp.Or(userID, c.RealIP())

	h.activeConnectionsMu.Lock()
	if h.activeConnections[clientKey] >= 5 {
		h.activeConnectionsMu.Unlock()
		return c.JSON(http.StatusTooManyRequests, map[string]any{"success": false, "error": "Too many concurrent stats connections for this client"})
	}
	if h.activeConnections == nil {
		h.activeConnections = make(map[string]int)
	}
	h.activeConnections[clientKey]++
	h.activeConnectionsMu.Unlock()
	defer func() {
		h.activeConnectionsMu.Lock()
		defer h.activeConnectionsMu.Unlock()
		if count := h.activeConnections[clientKey]; count > 1 {
			h.activeConnections[clientKey] = count - 1
		} else {
			delete(h.activeConnections, clientKey)
		}
	}()

	conn, unregister, ok := h.acceptWS(c, types.WSKindSystemStats, "")
	if !ok {
		return nil
	}
	defer unregister()
	defer func() {
		if err := conn.CloseNow(); err != nil {
			slog.DebugContext(c.Request().Context(), "Failed to close system stats websocket connection", "clientKey", clientKey, "error", err)
		}
	}()

	interval, _ := httputil.GetIntQueryParam(c.Request(), "interval", false)
	sampleInterval := time.Duration(min(cmp.Or(max(interval, 0), 2), 60)) * time.Second

	conn.SetReadLimit(512)

	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()
	if !h.acquireSystemStatsSampler(ctx, sampleInterval) {
		h.releaseSystemStatsSampler(sampleInterval)
		return nil
	}
	defer h.releaseSystemStatsSampler(sampleInterval)

	// Single reader for this connection; it also services the keepalive pong. Do not add others.
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				cancel()
				return
			}
		}
	}()
	go wshub.KeepAlive(ctx, cancel, conn)

	send := func() error {
		stats, _ := h.systemStatsSampler.latest.Load()
		b, err := json.Marshal(stats)
		if err != nil {
			return err
		}
		wctx, localCancel := context.WithTimeout(ctx, 10*time.Second)
		defer localCancel()
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
