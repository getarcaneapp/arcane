package volumes

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"kit.getarcane.app"
	"kit.getarcane.app/docker/compat"
)

func BindSubpaths(ctx context.Context, dockerClient *client.Client, mounts []mount.Mount) ([]mount.Mount, error) {
	if compat.IsDockerAPIVersionAtLeast(compat.DetectDockerAPIVersion(ctx, dockerClient), "1.45") {
		return mounts, nil
	}
	result := slices.Clone(mounts)
	for index, resolved := range result {
		if resolved.Type != mount.TypeVolume || kit.FromPtr(resolved.VolumeOptions).Subpath == "" {
			continue
		}
		inspect, err := dockerClient.VolumeInspect(ctx, resolved.Source, client.VolumeInspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("inspect volume %s: %w", resolved.Source, err)
		}
		result[index] = mount.Mount{
			Type:     mount.TypeBind,
			Source:   filepath.Join(inspect.Volume.Mountpoint, resolved.VolumeOptions.Subpath),
			Target:   resolved.Target,
			ReadOnly: resolved.ReadOnly,
		}
	}
	return result, nil
}
