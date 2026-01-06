package docker

import (
	"strings"

	containertypes "github.com/docker/docker/api/types/container"
	mounttypes "github.com/docker/docker/api/types/mount"
)

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
