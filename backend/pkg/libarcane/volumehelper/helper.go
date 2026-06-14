package volumehelper

import (
	"context"
	"os"
	"runtime"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

// RuntimeImage describes the Arcane runtime image that can run internal helper
// commands from inside temporary containers.
type RuntimeImage struct {
	Image      string
	Entrypoint []string
	Command    []string
	Source     string
}

const DefaultToolsImage = "ghcr.io/getarcaneapp/tools:latest"

// Labels returns the labels used for temporary internal volume helper containers.
func Labels() map[string]string {
	return map[string]string{
		libarcane.InternalResourceLabel: "true",
	}
}

// RemoveOptions returns the container remove options used for volume helpers.
func RemoveOptions() client.ContainerRemoveOptions {
	return client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}
}

// HostConfig builds the host config shared by volume helper containers.
func HostConfig(helperImage string, binds []string, mounts []mount.Mount) *container.HostConfig {
	hostConfig := &container.HostConfig{
		Binds:      binds,
		Mounts:     mounts,
		AutoRemove: true,
	}

	if runtime.GOOS == "linux" && isArcaneFallbackImage(helperImage) {
		hostConfig.Tmpfs = map[string]string{
			"/app/data": "rw,noexec,nosuid,nodev",
		}
	}

	return hostConfig
}

// ResolveArcaneRuntimeImage resolves the current Arcane or Arcane agent image
// so internal helper commands can run without pulling an external helper image.
func ResolveArcaneRuntimeImage(ctx context.Context, dockerClient *client.Client) (RuntimeImage, bool) {
	hostname, _ := os.Hostname()
	if hostname != "" {
		if inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, hostname, client.ContainerInspectOptions{}); err == nil && inspect.Container.Config != nil && strings.TrimSpace(inspect.Container.Config.Image) != "" {
			return buildRuntimeImage(inspect.Container.Config.Image, inspect.Container.Config.Entrypoint, inspect.Container.Config.Cmd, "hostname"), true
		}
	}

	for _, label := range []string{"com.getarcaneapp.arcane=true", "com.getarcaneapp.arcane.agent=true"} {
		filter := make(client.Filters)
		filter = filter.Add("label", label)
		containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{Filters: filter, All: true})
		if err != nil || len(containers.Items) == 0 {
			continue
		}

		if resolved, ok := resolveRuntimeImageFromContainers(ctx, dockerClient, containers.Items, label, true); ok {
			return resolved, true
		}
		if resolved, ok := resolveRuntimeImageFromContainers(ctx, dockerClient, containers.Items, label, false); ok {
			return resolved, true
		}
	}

	return RuntimeImage{}, false
}

func resolveRuntimeImageFromContainers(ctx context.Context, dockerClient *client.Client, containers []container.Summary, label string, runningOnly bool) (RuntimeImage, bool) {
	source := "arcane-label"
	if strings.Contains(label, ".agent=") {
		source = "arcane-agent-label"
	}

	for _, c := range containers {
		if runningOnly && c.State != container.StateRunning {
			continue
		}
		if !runningOnly && c.State == container.StateRunning {
			continue
		}
		inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, c.ID, client.ContainerInspectOptions{})
		if err == nil && inspect.Container.Config != nil && strings.TrimSpace(inspect.Container.Config.Image) != "" {
			return buildRuntimeImage(inspect.Container.Config.Image, inspect.Container.Config.Entrypoint, inspect.Container.Config.Cmd, source), true
		}
		if strings.TrimSpace(c.Image) != "" {
			return buildRuntimeImage(c.Image, nil, nil, source), true
		}
	}

	return RuntimeImage{}, false
}

func buildRuntimeImage(image string, entrypoint []string, command []string, source string) RuntimeImage {
	return RuntimeImage{
		Image:      strings.TrimSpace(image),
		Entrypoint: append([]string(nil), entrypoint...),
		Command:    append([]string(nil), command...),
		Source:     source,
	}
}

func isArcaneFallbackImage(helperImage string) bool {
	return !strings.EqualFold(strings.TrimSpace(helperImage), DefaultToolsImage)
}
