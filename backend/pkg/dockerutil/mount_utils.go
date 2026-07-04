package docker

import (
	"context"
	"os"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"go.getarcane.app/sys/cgroup"
)

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
func MountForCurrentContainerSubpath(ctx context.Context, dockerCli *client.Client, containerPath, target string) (*mount.Mount, error) {
	if dockerCli == nil {
		return nil, nil
	}
	inspectTarget, err := getCurrentContainerInspectTargetInternal(cgroup.CurrentContainerID, os.Hostname)
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
func MountForSubpath(mounts []container.MountPoint, containerPath string, target string) *mount.Mount {
	if strings.TrimSpace(containerPath) == "" {
		return nil
	}
	if strings.TrimSpace(target) == "" {
		target = containerPath
	}

	var best *container.MountPoint
	for i := range mounts {
		m := &mounts[i]
		if m.Destination == "" {
			continue
		}
		if !pathHasPrefixInternal(containerPath, m.Destination) {
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
	case mount.TypeBind:
		if strings.TrimSpace(best.Source) == "" {
			return nil
		}
		source := best.Source
		if relative != "" {
			source = strings.TrimRight(source, "/") + "/" + relative
		}
		return &mount.Mount{Type: mount.TypeBind, Source: source, Target: target, ReadOnly: readOnly}
	case mount.TypeVolume:
		if strings.TrimSpace(best.Name) == "" {
			return nil
		}
		m := &mount.Mount{Type: mount.TypeVolume, Source: best.Name, Target: target, ReadOnly: readOnly}
		if relative != "" {
			m.VolumeOptions = &mount.VolumeOptions{Subpath: relative}
		}
		return m
	default:
		return nil
	}
}

// pathHasPrefixInternal reports whether containerPath is at or under prefix,
// treating both as POSIX-style paths. Avoids false positives like
// "/app/datax" matching "/app/data".
func pathHasPrefixInternal(containerPath, prefix string) bool {
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
func MountForDestination(mounts []container.MountPoint, destination string, target string) *mount.Mount {
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
		case mount.TypeVolume:
			if strings.TrimSpace(m.Name) == "" {
				return nil
			}
			return &mount.Mount{Type: mount.TypeVolume, Source: m.Name, Target: target, ReadOnly: readOnly}
		case mount.TypeBind:
			if strings.TrimSpace(m.Source) == "" {
				return nil
			}
			return &mount.Mount{Type: mount.TypeBind, Source: m.Source, Target: target, ReadOnly: readOnly}
		case mount.TypeTmpfs:
			return nil
		case mount.TypeNamedPipe:
			return nil
		case mount.TypeCluster:
			return nil
		case mount.TypeImage:
			return nil
		default:
			return nil
		}
	}

	return nil
}
