package projects

import (
	"context"
	"os"
	"strings"

	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"go.getarcane.app/sys/cgroup"
)

// GetCurrentContainerMounts inspects Arcane's own container and returns its bind and
// named-volume mounts as HostMount entries. It returns no mounts when Arcane is
// not running in a container (or the daemon is unreachable). This is the basis for
// Docker-in-Docker host-path resolution.
func GetCurrentContainerMounts(ctx context.Context, dockerCli *client.Client) ([]HostMount, error) {
	if dockerCli == nil {
		return nil, nil // No docker client, can't discover
	}

	// Prefer robust current-container detection and fall back to hostname.
	inspectTarget, err := getCurrentContainerInspectTargetInternal(cgroup.CurrentContainerID, os.Hostname)
	if err != nil {
		return nil, err
	}

	inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerCli, inspectTarget, client.ContainerInspectOptions{})
	if err != nil {
		// Not running in a container or can't reach docker daemon
		return nil, err
	}

	mounts := make([]HostMount, 0, len(inspect.Container.Mounts))
	for i := range inspect.Container.Mounts {
		m := &inspect.Container.Mounts[i]
		if m.Type != mounttypes.TypeBind && m.Type != mounttypes.TypeVolume {
			continue
		}
		if strings.TrimSpace(m.Source) == "" || strings.TrimSpace(m.Destination) == "" {
			continue
		}
		mounts = append(mounts, HostMount{Destination: m.Destination, Source: m.Source})
	}
	return mounts, nil
}

// GetHostPathForContainerPath attempts to discover the host-side path for a given container path
// by inspecting the container itself. This is useful for Docker-in-Docker scenarios
// where the application needs to know host paths for volume mapping. It returns an empty
// string when the path is not covered by any of Arcane's mounts.
func GetHostPathForContainerPath(ctx context.Context, dockerCli *client.Client, containerPath string) (string, error) {
	mounts, err := GetCurrentContainerMounts(ctx, dockerCli)
	if err != nil {
		return "", err
	}

	if host, ok := ResolveHostPath(mounts, containerPath); ok {
		return host, nil
	}

	return "", nil
}

func getCurrentContainerInspectTargetInternal(currentContainerID func() (string, error), hostname func() (string, error)) (string, error) {
	if currentContainerID != nil {
		if containerID, err := currentContainerID(); err == nil {
			if containerID = strings.TrimSpace(containerID); containerID != "" {
				return containerID, nil
			}
		}
	}

	if hostname == nil {
		hostname = os.Hostname
	}

	value, err := hostname()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(value), nil
}
