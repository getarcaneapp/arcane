package volumes

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	mobyMount "github.com/moby/moby/api/types/mount"
	mobyClient "github.com/moby/moby/client"

	kit "kit.getarcane.app"
	"kit.getarcane.app/docker/compat"
)

func BindSubpaths(ctx context.Context, dockerClient *mobyClient.Client, mounts []mobyMount.Mount) ([]mobyMount.Mount, error) {
	if compat.IsDockerAPIVersionAtLeast(compat.DetectDockerAPIVersion(ctx, dockerClient), "1.45") {
		return mounts, nil
	}
	result := slices.Clone(mounts)
	for index, resolved := range result {
		if resolved.Type != mobyMount.TypeVolume || kit.FromPtr(resolved.VolumeOptions).Subpath == "" {
			continue
		}
		inspect, err := dockerClient.VolumeInspect(ctx, resolved.Source, mobyClient.VolumeInspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("inspect volume %s: %w", resolved.Source, err)
		}
		result[index] = mobyMount.Mount{
			Type:     mobyMount.TypeBind,
			Source:   filepath.Join(inspect.Volume.Mountpoint, resolved.VolumeOptions.Subpath),
			Target:   resolved.Target,
			ReadOnly: resolved.ReadOnly,
		}
	}
	return result, nil
}
