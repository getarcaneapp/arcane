package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

var (
	volumeUsageCacheMutex       sync.RWMutex
	volumeUsageCacheEntries     = make(map[string]*volumeUsageCacheEntryInternal)
	volumeUsageCacheGenerations = make(map[string]uint64)
)

const (
	volumeUsageCacheTTL               = 30 * time.Second
	volumeUsageRefreshFallbackTimeout = 30 * time.Second
)

type volumeUsageCacheEntryInternal struct {
	volumes     []volume.Volume
	refreshedAt time.Time
	generation  uint64
	refresh     *volumeUsageRefreshInternal
}

type volumeUsageRefreshInternal struct {
	done chan struct{}
	err  error
}

// GetVolumeUsageData returns current volume usage data, sharing an in-flight
// refresh with concurrent callers while allowing each caller to honor its own context.
func GetVolumeUsageData(ctx context.Context, dockerClient *client.Client) ([]volume.Volume, error) {
	if dockerClient == nil {
		return nil, errors.New("failed to get disk usage: Docker client is nil")
	}

	key := volumeUsageCacheKeyInternal(dockerClient)
	for {
		cached, _, fresh := volumeUsageCacheSnapshotInternal(key)
		if fresh {
			slog.DebugContext(ctx, "returning cached volume usage data", "volume_count", len(cached), "docker_host", key)
			return cached, nil
		}

		refresh := startVolumeUsageRefreshInternal(ctx, key, dockerClient)
		select {
		case <-ctx.Done():
			if stale, staleFound, _ := volumeUsageCacheSnapshotInternal(key); staleFound {
				slog.WarnContext(ctx, "volume usage refresh timed out; returning stale cache", "error", ctx.Err(), "volume_count", len(stale), "docker_host", key)
				return stale, nil
			}
			return nil, ctx.Err()
		case <-refresh.done:
			updated, updatedFound, _ := volumeUsageCacheSnapshotInternal(key)
			if updatedFound {
				if refresh.err != nil {
					slog.WarnContext(ctx, "volume usage refresh failed; returning stale cache", "error", refresh.err, "volume_count", len(updated), "docker_host", key)
				}
				return updated, nil
			}
			if refresh.err != nil {
				return nil, refresh.err
			}
			// The cache was invalidated while the refresh was running. Retry against
			// the new generation rather than publishing obsolete usage data.
		}
	}
}

// GetVolumeUsageDataStaleWhileRevalidate returns immediately with any cached
// snapshot and starts a bounded refresh when the snapshot is stale or missing.
func GetVolumeUsageDataStaleWhileRevalidate(ctx context.Context, dockerClient *client.Client) ([]volume.Volume, bool) {
	if dockerClient == nil {
		return nil, false
	}

	key := volumeUsageCacheKeyInternal(dockerClient)
	cached, found, fresh := volumeUsageCacheSnapshotInternal(key)
	cacheState := "miss"
	if found {
		cacheState = "stale"
		if fresh {
			cacheState = "fresh"
		}
	}
	slog.DebugContext(ctx, "volume usage cache lookup",
		"docker_host", key,
		"cache_state", cacheState,
		"volume_count", len(cached),
		"refresh_requested", !fresh,
	)
	if !fresh {
		startVolumeUsageRefreshInternal(ctx, key, dockerClient)
	}
	return cached, found
}

// InvalidateVolumeUsageCache invalidates usage data for the Docker daemon used by dockerClient.
func InvalidateVolumeUsageCache(dockerClient *client.Client) {
	if dockerClient == nil {
		return
	}

	key := volumeUsageCacheKeyInternal(dockerClient)
	volumeUsageCacheMutex.Lock()
	volumeUsageCacheGenerations[key]++
	delete(volumeUsageCacheEntries, key)
	volumeUsageCacheMutex.Unlock()
}

func volumeUsageCacheKeyInternal(dockerClient *client.Client) string {
	host := dockerClient.DaemonHost()
	if host != "" {
		return host
	}
	return fmt.Sprintf("client:%p", dockerClient)
}

func volumeUsageCacheSnapshotInternal(key string) ([]volume.Volume, bool, bool) {
	volumeUsageCacheMutex.RLock()
	entry := volumeUsageCacheEntries[key]
	if entry == nil || entry.volumes == nil {
		volumeUsageCacheMutex.RUnlock()
		return nil, false, false
	}
	volumes := append([]volume.Volume(nil), entry.volumes...)
	fresh := time.Since(entry.refreshedAt) < volumeUsageCacheTTL
	volumeUsageCacheMutex.RUnlock()
	return volumes, true, fresh
}

func startVolumeUsageRefreshInternal(ctx context.Context, key string, dockerClient *client.Client) *volumeUsageRefreshInternal {
	volumeUsageCacheMutex.Lock()
	generation := volumeUsageCacheGenerations[key]
	entry := volumeUsageCacheEntries[key]
	if entry == nil || entry.generation != generation {
		entry = &volumeUsageCacheEntryInternal{generation: generation}
		volumeUsageCacheEntries[key] = entry
	}
	if entry.refresh != nil {
		refresh := entry.refresh
		volumeUsageCacheMutex.Unlock()
		return refresh
	}

	refresh := &volumeUsageRefreshInternal{done: make(chan struct{})}
	entry.refresh = refresh
	volumeUsageCacheMutex.Unlock()

	refreshCtx, cancel := volumeUsageRefreshContextInternal(ctx)
	go func() {
		defer cancel()

		volumes, err := fetchVolumeUsageDataInternal(refreshCtx, dockerClient)

		volumeUsageCacheMutex.Lock()
		current := volumeUsageCacheEntries[key]
		if current != nil && current.generation == generation && current.refresh == refresh {
			if err == nil {
				current.volumes = volumes
				current.refreshedAt = time.Now()
			}
			current.refresh = nil
		}
		refresh.err = err
		close(refresh.done)
		volumeUsageCacheMutex.Unlock()

		if err != nil {
			slog.WarnContext(refreshCtx, "failed to refresh volume usage cache", "error", err, "docker_host", key)
			return
		}
		slog.DebugContext(refreshCtx, "refreshed volume usage cache", "volume_count", len(volumes), "docker_host", key)
	}()

	return refresh
}

func volumeUsageRefreshContextInternal(ctx context.Context) (context.Context, context.CancelFunc) {
	detached := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(detached, deadline)
	}
	return context.WithTimeout(detached, volumeUsageRefreshFallbackTimeout)
}

func fetchVolumeUsageDataInternal(ctx context.Context, dockerClient *client.Client) ([]volume.Volume, error) {
	diskUsage, err := dockerClient.DiskUsage(ctx, client.DiskUsageOptions{
		Volumes: true,
		Verbose: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get disk usage: %w", err)
	}

	slog.DebugContext(ctx, "disk usage returned volumes", "volume_count", len(diskUsage.Volumes.Items))
	return append([]volume.Volume{}, diskUsage.Volumes.Items...), nil
}

// FilterContainersUsingVolume returns the IDs of containers in the provided slice that mount the named volume.
// Use this when checking many volumes in a row — list containers once and reuse the slice.
func FilterContainersUsingVolume(containers []container.Summary, volumeName string) []string {
	containerIDs := make([]string, 0)
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name == volumeName {
				containerIDs = append(containerIDs, c.ID)
				break
			}
		}
	}
	return containerIDs
}

// GetContainersUsingVolume lists all containers and returns IDs that mount the named volume.
// For batch checks (multiple volumes), prefer listing containers once via dockerClient.ContainerList
// and calling FilterContainersUsingVolume per volume.
func GetContainersUsingVolume(ctx context.Context, dockerClient *client.Client, volumeName string) ([]string, error) {
	containerList, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	containerIDs := FilterContainersUsingVolume(containerList.Items, volumeName)
	slog.DebugContext(ctx, "found containers using volume", "volume", volumeName, "container_count", len(containerIDs))
	return containerIDs, nil
}
