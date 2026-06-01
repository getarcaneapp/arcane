package docker

import (
	"context"
	"os"
	"strings"

	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/pkg/projects"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

// GetHostPathForContainerPath attempts to discover the host-side path for a given container path
// by inspecting the container itself. This is useful for Docker-in-Docker scenarios
// where the application needs to know host paths for volume mapping.
func GetHostPathForContainerPath(ctx context.Context, dockerCli *client.Client, containerPath string) (string, error) {
	if dockerCli == nil {
		return "", nil // No docker client, can't discover
	}

	// 1. Prefer robust current-container detection and fall back to hostname.
	inspectTarget, err := getCurrentContainerInspectTargetInternal(GetCurrentContainerID, os.Hostname)
	if err != nil {
		return "", err
	}

	// 2. Inspect self
	inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerCli, inspectTarget, client.ContainerInspectOptions{})
	if err != nil {
		// Not running in a container or can't reach docker daemon
		return "", err
	}

	// 3. Find mount point for the target path
	// We want to find the mount that most specifically matches our path
	var bestMatch *containertypes.MountPoint
	for i := range inspect.Container.Mounts {
		m := &inspect.Container.Mounts[i]
		if strings.HasPrefix(containerPath, m.Destination) {
			if bestMatch == nil || len(m.Destination) > len(bestMatch.Destination) {
				bestMatch = m
			}
		}
	}

	if bestMatch != nil && (bestMatch.Type == mounttypes.TypeBind || bestMatch.Type == mounttypes.TypeVolume) {
		// Calculate the relative path from mount destination to target path
		rel := strings.TrimPrefix(containerPath, bestMatch.Destination)
		rel = strings.TrimPrefix(rel, "/") // Ensure no double slash

		hostPath := bestMatch.Source
		if rel != "" {
			// Determine path separator from the host path
			separator := "/"
			if projects.IsWindowsDrivePath(hostPath) && strings.Contains(hostPath, "\\") {
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

// MountForCurrentContainerSubpath inspects the current container, finds the
// existing mount whose destination covers containerPath, and returns a Mount
// suitable for use in another container creation that exposes the same data
// at target. Returns nil + no error if Arcane isn't running inside a
// container or no suitable mount is found — callers can fall back to a
// plain bind on containerPath in that case.
func MountForCurrentContainerSubpath(ctx context.Context, dockerCli *client.Client, containerPath, target string) (*mounttypes.Mount, error) {
	if dockerCli == nil {
		return nil, nil
	}
	inspectTarget, err := getCurrentContainerInspectTargetInternal(GetCurrentContainerID, os.Hostname)
	if err != nil {
		return nil, err
	}
	inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerCli, inspectTarget, client.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}
	return MountForSubpath(inspect.Container.Mounts, containerPath, target), nil
}

// MountForSubpath returns a Mount that exposes a subpath of one of the
// current container's existing mounts at the requested target. It's a
// generalisation of MountForDestination for the case where the caller
// wants a sub-tree below an existing mount destination (e.g.
// "/app/data/projects/X" when "/app/data" is what the container has
// mounted).
//
// The function picks the most-specific mount whose Destination is a
// prefix of containerPath, then constructs the Mount based on the
// backing type:
//
//   - TypeBind:   Source = mount.Source joined with the relative subpath.
//     Works because bind sources are real host paths the daemon
//     can address directly.
//   - TypeVolume: Source = mount.Name (the volume name), and the relative
//     subpath is set on VolumeOptions.Subpath. This lets the
//     daemon mount the named volume directly without needing a
//     host-side path translation — important for setups where
//     the underlying volume storage is opaque (Docker Desktop
//     on WSL2, Docker-in-Docker, etc.).
//
// Returns nil if no mount destination is a prefix of containerPath or if
// the matching mount is of an unsupported type.
func MountForSubpath(mounts []containertypes.MountPoint, containerPath string, target string) *mounttypes.Mount {
	if strings.TrimSpace(containerPath) == "" {
		return nil
	}
	if strings.TrimSpace(target) == "" {
		target = containerPath
	}

	var best *containertypes.MountPoint
	for i := range mounts {
		m := &mounts[i]
		if m.Destination == "" {
			continue
		}
		if !pathHasPrefix(containerPath, m.Destination) {
			continue
		}
		if best == nil || len(m.Destination) > len(best.Destination) {
			best = m
		}
	}
	if best == nil {
		return nil
	}

	relative := strings.TrimPrefix(strings.TrimPrefix(containerPath, best.Destination), "/")
	readOnly := !best.RW

	switch best.Type { //nolint:exhaustive // only bind and volume mounts are translatable; the default returns nil for the rest
	case mounttypes.TypeBind:
		if strings.TrimSpace(best.Source) == "" {
			return nil
		}
		source := best.Source
		if relative != "" {
			source = strings.TrimRight(source, "/") + "/" + relative
		}
		return &mounttypes.Mount{Type: mounttypes.TypeBind, Source: source, Target: target, ReadOnly: readOnly}
	case mounttypes.TypeVolume:
		if strings.TrimSpace(best.Name) == "" {
			return nil
		}
		m := &mounttypes.Mount{Type: mounttypes.TypeVolume, Source: best.Name, Target: target, ReadOnly: readOnly}
		if relative != "" {
			m.VolumeOptions = &mounttypes.VolumeOptions{Subpath: relative}
		}
		return m
	default:
		return nil
	}
}

// pathHasPrefix reports whether containerPath is at or under prefix,
// treating both as POSIX-style paths. Avoids false positives like
// "/app/datax" matching "/app/data".
func pathHasPrefix(containerPath, prefix string) bool {
	if containerPath == prefix {
		return true
	}
	p := strings.TrimRight(prefix, "/") + "/"
	return strings.HasPrefix(containerPath, p)
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
