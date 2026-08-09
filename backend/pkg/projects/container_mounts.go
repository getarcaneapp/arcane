package projects

import (
	"context"
	"strings"

	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
)

// GetCurrentContainerMounts inspects Arcane's own container and returns its bind and
// named-volume mounts as HostMount entries. It returns no mounts when Arcane is
// not running in a container (or the daemon is unreachable). This is the basis for
// Docker-in-Docker host-path resolution.
func GetCurrentContainerMounts(ctx context.Context, dockerCli *client.Client) ([]HostMount, error) {
	if dockerCli == nil {
		return nil, nil // No docker client, can't discover
	}

	// Label-fallback detection matters here: with network_mode: service:<sidecar>
	// the hostname identifies the sidecar, whose mounts do not include
	// docker.sock — breaking host-path resolution for the self-upgrader (#3544).
	inspect, err := libarcane.InspectCurrentArcaneContainer(ctx, dockerCli)
	if err != nil {
		// Not running in a container or can't reach docker daemon
		return nil, err
	}

	mounts := make([]HostMount, 0, len(inspect.Mounts))
	for i := range inspect.Mounts {
		m := &inspect.Mounts[i]
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

	if host, ok := ResolveHostPath(mounts, containerPath).Get(); ok {
		return host, nil
	}

	return "", nil
}
