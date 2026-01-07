package docker

import (
	"context"
	"os"
	"strings"

	containertypes "github.com/docker/docker/api/types/container"
	mounttypes "github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pathmapper"
)

// GetHostPathForContainerPath attempts to discover the host-side path for a given container path
// by inspecting the container itself. This is useful for Docker-in-Docker scenarios
// where the application needs to know host paths for volume mapping.
func GetHostPathForContainerPath(ctx context.Context, dockerCli *client.Client, containerPath string) (string, error) {
	if dockerCli == nil {
		return "", nil // No docker client, can't discover
	}

	// 1. Get current container ID (usually the short ID is the hostname)
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}

	// 2. Inspect self
	inspect, err := dockerCli.ContainerInspect(ctx, hostname)
	if err != nil {
		// Not running in a container or can't reach docker daemon
		return "", err
	}

	// 3. Find mount point for the target path
	// We want to find the mount that most specifically matches our path
	var bestMatch *containertypes.MountPoint
	for i := range inspect.Mounts {
		m := &inspect.Mounts[i]
		if strings.HasPrefix(containerPath, m.Destination) {
			if bestMatch == nil || len(m.Destination) > len(bestMatch.Destination) {
				bestMatch = m
			}
		}
	}

	if bestMatch != nil && bestMatch.Type == mounttypes.TypeBind {
		// Calculate the relative path from mount destination to target path
		rel := strings.TrimPrefix(containerPath, bestMatch.Destination)
		rel = strings.TrimPrefix(rel, "/") // Ensure no double slash

		hostPath := bestMatch.Source
		if rel != "" {
			// Determine path separator from the host path
			separator := "/"
			if pathmapper.IsWindowsDrivePath(hostPath) && strings.Contains(hostPath, "\\") {
				separator = "\\"
				rel = strings.ReplaceAll(rel, "/", "\\")
			}

			if !strings.HasSuffix(hostPath, separator) {
				hostPath += separator
			}
			hostPath += rel
		}
		return hostPath, nil
	}

	return "", nil
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
