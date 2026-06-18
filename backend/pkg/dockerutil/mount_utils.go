package docker

import (
	"context"
	"os"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

// GetCurrentContainerMounts inspects Arcane's own container and returns its bind and
// named-volume mounts as projects.HostMount entries. It returns no mounts when Arcane is
// not running in a container (or the daemon is unreachable). This is the basis for
// Docker-in-Docker host-path resolution.
func GetCurrentContainerMounts(ctx context.Context, dockerCli *client.Client) ([]projects.HostMount, error) {
	if dockerCli == nil {
		return nil, nil // No docker client, can't discover
	}

	// Prefer robust current-container detection and fall back to hostname.
	inspectTarget, err := getCurrentContainerInspectTargetInternal(GetCurrentContainerID, os.Hostname)
	if err != nil {
		return nil, err
	}

	inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerCli, inspectTarget, client.ContainerInspectOptions{})
	if err != nil {
		// Not running in a container or can't reach docker daemon
		return nil, err
	}

	mounts := make([]projects.HostMount, 0, len(inspect.Container.Mounts))
	for i := range inspect.Container.Mounts {
		m := &inspect.Container.Mounts[i]
		if m.Type != mounttypes.TypeBind && m.Type != mounttypes.TypeVolume {
			continue
		}
		if strings.TrimSpace(m.Source) == "" || strings.TrimSpace(m.Destination) == "" {
			continue
		}
		mounts = append(mounts, projects.HostMount{Destination: m.Destination, Source: m.Source})
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

	if host, ok := projects.ResolveHostPath(mounts, containerPath); ok {
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

// MountForDestination returns a Mount suitable for container creation that mirrors an
// existing container mount at the given destination.
//
// It currently supports bind and named volume mounts. If target is empty, destination
// is used as the target.
func MountForDestination(mounts []containertypes.MountPoint, destination string, target string) *mounttypes.Mount {
	if strings.TrimSpace(destination) == "" {
		return nil
	}
	if strings.TrimSpace(target) == "" {
		target = destination
	}

	for _, m := range mounts {
		if m.Destination != destination {
			continue
		}

		readOnly := !m.RW

		switch m.Type {
		case mounttypes.TypeVolume:
			if strings.TrimSpace(m.Name) == "" {
				return nil
			}
			return &mounttypes.Mount{Type: mounttypes.TypeVolume, Source: m.Name, Target: target, ReadOnly: readOnly}
		case mounttypes.TypeBind:
			if strings.TrimSpace(m.Source) == "" {
				return nil
			}
			return &mounttypes.Mount{Type: mounttypes.TypeBind, Source: m.Source, Target: target, ReadOnly: readOnly}
		case mounttypes.TypeTmpfs:
			return nil
		case mounttypes.TypeNamedPipe:
			return nil
		case mounttypes.TypeCluster:
			return nil
		case mounttypes.TypeImage:
			return nil
		default:
			return nil
		}
	}

	return nil
}
