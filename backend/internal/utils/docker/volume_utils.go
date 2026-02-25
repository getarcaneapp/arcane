package docker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

var (
	volumeUsageCache      []volume.Volume
	volumeUsageCacheMutex sync.RWMutex
	volumeUsageCacheTime  time.Time
	volumeUsageCacheTTL   = 30 * time.Second
)

func GetVolumeUsageData(ctx context.Context, dockerClient *client.Client) ([]volume.Volume, error) {
	volumeUsageCacheMutex.RLock()
	if time.Since(volumeUsageCacheTime) < volumeUsageCacheTTL && volumeUsageCache != nil {
		cached := volumeUsageCache
		volumeUsageCacheMutex.RUnlock()
		slog.DebugContext(ctx, "returning cached volume usage data", "volume_count", len(cached))
		return cached, nil
	}
	volumeUsageCacheMutex.RUnlock()

	volumeUsageCacheMutex.Lock()
	defer volumeUsageCacheMutex.Unlock()

	if time.Since(volumeUsageCacheTime) < volumeUsageCacheTTL && volumeUsageCache != nil {
		slog.DebugContext(ctx, "returning cached volume usage data after lock", "volume_count", len(volumeUsageCache))
		return volumeUsageCache, nil
	}
	diskUsage, err := dockerClient.DiskUsage(ctx, client.DiskUsageOptions{
		Volumes: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get disk usage: %w", err)
	}

	slog.DebugContext(ctx, "disk usage returned volumes", "volume_count", len(diskUsage.Volumes.Items))

	if len(diskUsage.Volumes.Items) == 0 {
		return []volume.Volume{}, nil
	}

	volumes := make([]volume.Volume, 0, len(diskUsage.Volumes.Items))
	volumes = append(volumes, diskUsage.Volumes.Items...)

	volumeUsageCache = volumes
	volumeUsageCacheTime = time.Now()
	slog.DebugContext(ctx, "refreshed volume usage cache", "volume_count", len(volumes))

	return volumes, nil
}

func InvalidateVolumeUsageCache() {
	volumeUsageCacheMutex.Lock()
	defer volumeUsageCacheMutex.Unlock()
	volumeUsageCache = nil
	volumeUsageCacheTime = time.Time{}
}

func GetContainersUsingVolume(ctx context.Context, dockerClient *client.Client, volumeName string) ([]string, error) {
	containerList, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	containers := containerList.Items

	containerIDs := make([]string, 0)
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name == volumeName {
				containerIDs = append(containerIDs, c.ID)
				break
			}
		}
	}

	slog.DebugContext(ctx, "found containers using volume", "volume", volumeName, "container_count", len(containerIDs))

	return containerIDs, nil
}
