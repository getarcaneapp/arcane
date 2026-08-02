package docker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"emperror.dev/errors"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/samber/hot"
	"github.com/samber/mo"
)

var volumeUsageCache = hot.NewHotCache[string, []volume.Volume](hot.LRU, 16).
	WithTTL(volumeUsageCacheTTL).
	WithCopyOnRead(cloneVolumeUsageInternal).
	WithCopyOnWrite(cloneVolumeUsageInternal).
	Build()

const (
	volumeUsageCacheTTL               = 30 * time.Second
	volumeUsageRefreshFallbackTimeout = 30 * time.Second
)

// GetVolumeUsageData returns current volume usage data, sharing an in-flight
// refresh with concurrent callers while allowing each caller to honor its own context.
func GetVolumeUsageData(ctx context.Context, dockerClient *client.Client) ([]volume.Volume, error) {
	if dockerClient == nil {
		return nil, errors.New("failed to get disk usage: Docker client is nil")
	}

	key := volumeUsageCacheKeyInternal(dockerClient)
	stale, staleFound := volumeUsageCache.Peek(key)
	if cached, found, _ := volumeUsageCache.Get(key); found {
		slog.DebugContext(ctx, "returning cached volume usage data", "volume_count", len(cached), "docker_host", key)
		return cached, nil
	}

	type refreshResult struct {
		volumes []volume.Volume
		found   bool
		err     error
	}
	result := make(chan refreshResult, 1)
	refreshCtx, cancel := volumeUsageRefreshContextInternal(ctx)
	go func() {
		defer cancel()
		volumes, found, err := volumeUsageCache.GetWithLoaders(key, func(_ []string) (map[string][]volume.Volume, error) {
			loaded, loadErr := fetchVolumeUsageDataInternal(refreshCtx, dockerClient)
			if loadErr != nil {
				return nil, loadErr
			}
			return map[string][]volume.Volume{key: loaded}, nil
		})
		result <- refreshResult{volumes: volumes, found: found, err: err}
	}()

	select {
	case <-ctx.Done():
		if staleFound {
			slog.WarnContext(ctx, "volume usage refresh timed out; returning stale cache", "error", ctx.Err(), "volume_count", len(stale), "docker_host", key)
			return stale, nil
		}
		return nil, ctx.Err()
	case refreshed := <-result:
		if refreshed.err != nil {
			if staleFound {
				slog.WarnContext(ctx, "volume usage refresh failed; returning stale cache", "error", refreshed.err, "volume_count", len(stale), "docker_host", key)
				return stale, nil
			}
			return nil, refreshed.err
		}
		if !refreshed.found {
			return nil, errors.New("volume usage cache loader returned no data")
		}
		return refreshed.volumes, nil
	}
}

// GetVolumeUsageDataStaleWhileRevalidate returns immediately with any cached
// snapshot and starts a bounded refresh when the snapshot is stale or missing.
func GetVolumeUsageDataStaleWhileRevalidate(ctx context.Context, dockerClient *client.Client) mo.Option[[]volume.Volume] {
	if dockerClient == nil {
		return mo.None[[]volume.Volume]()
	}

	key := volumeUsageCacheKeyInternal(dockerClient)
	cached, found := volumeUsageCache.Peek(key)
	if fresh, freshFound, _ := volumeUsageCache.Get(key); freshFound {
		slog.DebugContext(ctx, "volume usage cache lookup",
			"docker_host", key,
			"cache_state", "fresh",
			"volume_count", len(fresh),
			"refresh_requested", false,
		)
		return mo.Some(fresh)
	}
	cacheState := "miss"
	if found {
		cacheState = "stale"
	}
	slog.DebugContext(ctx, "volume usage cache lookup",
		"docker_host", key,
		"cache_state", cacheState,
		"volume_count", len(cached),
		"refresh_requested", true,
	)
	refreshCtx, cancel := volumeUsageRefreshContextInternal(ctx)
	go func() {
		defer cancel()
		_, _, err := volumeUsageCache.GetWithLoaders(key, func(_ []string) (map[string][]volume.Volume, error) {
			loaded, loadErr := fetchVolumeUsageDataInternal(refreshCtx, dockerClient)
			if loadErr != nil {
				return nil, loadErr
			}
			return map[string][]volume.Volume{key: loaded}, nil
		})
		if err != nil {
			slog.WarnContext(refreshCtx, "failed to refresh volume usage cache", "error", err, "docker_host", key)
		}
	}()
	if !found {
		return mo.None[[]volume.Volume]()
	}
	return mo.Some(cached)
}

// InvalidateVolumeUsageCache invalidates usage data for the Docker daemon used by dockerClient.
func InvalidateVolumeUsageCache(dockerClient *client.Client) {
	if dockerClient == nil {
		return
	}

	key := volumeUsageCacheKeyInternal(dockerClient)
	volumeUsageCache.Delete(key)
}

func volumeUsageCacheKeyInternal(dockerClient *client.Client) string {
	host := dockerClient.DaemonHost()
	if host != "" {
		return host
	}
	return fmt.Sprintf("client:%p", dockerClient)
}

func volumeUsageRefreshContextInternal(ctx context.Context) (context.Context, context.CancelFunc) {
	detached := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(detached, deadline)
	}
	return context.WithTimeout(detached, volumeUsageRefreshFallbackTimeout)
}

func cloneVolumeUsageInternal(volumes []volume.Volume) []volume.Volume {
	return append([]volume.Volume(nil), volumes...)
}

func fetchVolumeUsageDataInternal(ctx context.Context, dockerClient *client.Client) ([]volume.Volume, error) {
	diskUsage, err := dockerClient.DiskUsage(ctx, client.DiskUsageOptions{
		Volumes: true,
		Verbose: true,
	})
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get disk usage")
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
		return nil, errors.WrapIf(err, "failed to list containers")
	}

	containerIDs := FilterContainersUsingVolume(containerList.Items, volumeName)
	slog.DebugContext(ctx, "found containers using volume", "volume", volumeName, "container_count", len(containerIDs))
	return containerIDs, nil
}
