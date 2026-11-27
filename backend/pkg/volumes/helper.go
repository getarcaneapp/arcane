package volumes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"

	ref "github.com/distribution/reference"
	mobyContainer "github.com/moby/moby/api/types/container"
	mobyMount "github.com/moby/moby/api/types/mount"
	mobyClient "github.com/moby/moby/client"
	"github.com/samber/mo"

	kit "kit.getarcane.app"
	"kit.getarcane.app/docker/compat"

	"getarcane.dev/arcane/v3/pkg/registry"
	"getarcane.dev/arcane/v3/pkg/selfcontainer"
)

type (
	HelperHost struct {
		Image       string
		NetworkMode mobyContainer.NetworkMode
		Binds       []string
		Mounts      []mobyMount.Mount
		Keep        bool
	}

	RuntimeImage struct {
		Image      string
		Source     string
		Entrypoint []string
		Command    []string
	}
)

const (
	toolsRepoPath   = "getarcaneapp/tools"
	ToolsImageLabel = "org.opencontainers.image.source=https://github.com/getarcaneapp/tools"
	ContainerLabel  = "com.getarcaneapp.volume-helper"
)

func ToolsImage(registrySetting string) string {
	return registry.ArcaneRegistryHost(registrySetting) + "/" + toolsRepoPath + ":latest"
}

func Labels() map[string]string {
	return map[string]string{
		selfcontainer.InternalResourceLabel: "true",
		ContainerLabel:                      "true",
	}
}

func RemoveOptions() mobyClient.ContainerRemoveOptions {
	return mobyClient.ContainerRemoveOptions{Force: true, RemoveVolumes: true}
}

func IsHelperContainer(c mobyContainer.Summary) bool {
	if internal, _ := kit.ParseBool(c.Labels[selfcontainer.InternalResourceLabel]); !internal {
		return false
	}
	if strings.EqualFold(c.Labels[ContainerLabel], "true") {
		return true
	}
	command := strings.ToLower(c.Command)
	return strings.Contains(command, "sleep") && strings.Contains(command, "infinity") && slices.ContainsFunc(c.Mounts, func(mounted mobyContainer.MountPoint) bool {
		return mounted.Destination == "/volume"
	})
}

func ListRunningContainers(ctx context.Context, dockerClient *mobyClient.Client) ([]mobyContainer.Summary, error) {
	running, err := dockerClient.ContainerList(ctx, mobyClient.ContainerListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	return slices.DeleteFunc(running.Items, IsHelperContainer), nil
}

func HostConfig(host HelperHost) *mobyContainer.HostConfig {
	var tmpfs map[string]string
	if runtime.GOOS == "linux" {
		if named, err := ref.ParseNormalizedNamed(strings.TrimSpace(host.Image)); err != nil || ref.Path(named) != toolsRepoPath {
			tmpfs = map[string]string{
				"/app/data": "rw,noexec,nosuid,nodev",
			}
		}
	}

	return &mobyContainer.HostConfig{
		Binds:       host.Binds,
		Mounts:      host.Mounts,
		AutoRemove:  !host.Keep,
		NetworkMode: host.NetworkMode,
		Tmpfs:       tmpfs,
	}
}

func ResolveHelperImage(ctx context.Context, dockerClient *mobyClient.Client, toolsImage string) (string, error) {
	if _, err := dockerClient.ImageInspect(ctx, toolsImage); err == nil {
		return toolsImage, nil
	}

	pullReader, pullErr := dockerClient.ImagePull(ctx, toolsImage, mobyClient.ImagePullOptions{})
	if pullErr == nil {
		if pullReader == nil {
			pullErr = errors.New("helper image pull returned no response body")
		} else {
			defer func() { _ = pullReader.Close() }()
			if _, err := io.Copy(io.Discard, pullReader); err != nil {
				pullErr = fmt.Errorf("read helper image pull response: %w", err)
			} else {
				return toolsImage, nil
			}
		}
	}

	if fallback, ok := ResolveArcaneRuntimeImage(ctx, dockerClient).Get(); ok && strings.TrimSpace(fallback.Image) != "" {
		return fallback.Image, nil
	}

	return "", fmt.Errorf("failed to resolve helper image: tools image unavailable and arcane fallback not found: %w", pullErr)
}

func ResolveArcaneRuntimeImage(ctx context.Context, dockerClient *mobyClient.Client) mo.Option[RuntimeImage] {
	hostname, _ := os.Hostname()
	if hostname != "" {
		if inspect,
			err := compat.ContainerInspectWithCompatibility(ctx,
			dockerClient,
			hostname,
			mobyClient.ContainerInspectOptions{}); err == nil && inspect.Container.Config != nil && strings.TrimSpace(inspect.Container.Config.Image) != "" {
			return mo.Some(buildRuntimeImage(inspect.Container.Config.Image, inspect.Container.Config.Entrypoint, inspect.Container.Config.Cmd, "hostname"))
		}
	}

	for _, label := range []string{"com.getarcaneapp.arcane=true", "com.getarcaneapp.arcane.agent=true"} {
		containers, err := dockerClient.ContainerList(ctx, mobyClient.ContainerListOptions{
			Filters: mobyClient.Filters{}.Add("label", label),
			All:     true,
		})
		if err != nil || len(containers.Items) == 0 {
			continue
		}

		if resolved, ok := resolveRuntimeImageFromContainers(ctx, dockerClient, containers.Items, label, true).Get(); ok {
			return mo.Some(resolved)
		}
		if resolved, ok := resolveRuntimeImageFromContainers(ctx, dockerClient, containers.Items, label, false).Get(); ok {
			return mo.Some(resolved)
		}
	}

	return mo.None[RuntimeImage]()
}

func resolveRuntimeImageFromContainers(ctx context.Context, dockerClient *mobyClient.Client, containers []mobyContainer.Summary, label string, runningOnly bool) mo.Option[RuntimeImage] {
	source := kit.Ternary(strings.Contains(label, ".agent="), "arcane-agent-label", "arcane-label")

	for _, c := range containers {
		if runningOnly && c.State != mobyContainer.StateRunning {
			continue
		}
		if !runningOnly && c.State == mobyContainer.StateRunning {
			continue
		}
		inspect, err := compat.ContainerInspectWithCompatibility(ctx, dockerClient, c.ID, mobyClient.ContainerInspectOptions{})
		if err == nil && inspect.Container.Config != nil && strings.TrimSpace(inspect.Container.Config.Image) != "" {
			return mo.Some(buildRuntimeImage(inspect.Container.Config.Image, inspect.Container.Config.Entrypoint, inspect.Container.Config.Cmd, source))
		}
		if strings.TrimSpace(c.Image) != "" {
			return mo.Some(buildRuntimeImage(c.Image, nil, nil, source))
		}
	}

	return mo.None[RuntimeImage]()
}

func buildRuntimeImage(image string, entrypoint, command []string, source string) RuntimeImage {
	return RuntimeImage{
		Image:      strings.TrimSpace(image),
		Entrypoint: append([]string(nil), entrypoint...),
		Command:    append([]string(nil), command...),
		Source:     source,
	}
}
