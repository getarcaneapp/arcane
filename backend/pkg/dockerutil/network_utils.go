package docker

import (
	"context"
	"log/slog"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const defaultNetworkModeInternal = "bridge"

func IsDefaultNetwork(name string) bool {
	switch name {
	case "bridge", "host", "none", "ingress":
		return true
	default:
		return false
	}
}

func SelectAutoNetworkMode(inspect *container.InspectResponse) string {
	if inspect == nil {
		return defaultNetworkModeInternal
	}

	if inspect.HostConfig != nil {
		networkMode := strings.TrimSpace(string(inspect.HostConfig.NetworkMode))
		if networkMode != "" && networkMode != "default" && networkMode != defaultNetworkModeInternal &&
			!container.NetworkMode(networkMode).IsContainer() {
			return networkMode
		}
	}

	if inspect.NetworkSettings != nil && len(inspect.NetworkSettings.Networks) > 0 {
		networkNames := make([]string, 0, len(inspect.NetworkSettings.Networks))
		for networkName := range inspect.NetworkSettings.Networks {
			networkName = strings.TrimSpace(networkName)
			if networkName != "" {
				networkNames = append(networkNames, networkName)
			}
		}
		sort.Strings(networkNames)

		for _, networkName := range networkNames {
			if !IsDefaultNetwork(networkName) {
				return networkName
			}
		}

		for _, networkName := range networkNames {
			if container.NetworkMode(networkName).IsContainer() {
				continue
			}
			if networkName == "host" || networkName == "none" || networkName == defaultNetworkModeInternal {
				return networkName
			}
		}
	}

	if inspect.HostConfig != nil {
		networkMode := strings.TrimSpace(string(inspect.HostConfig.NetworkMode))
		if networkMode != "" && networkMode != "default" && !container.NetworkMode(networkMode).IsContainer() {
			return networkMode
		}
	}

	return defaultNetworkModeInternal
}

// SelectDockerHostReachableNetworkMode picks the network for a helper container
// that must reach a tcp DOCKER_HOST (e.g. a docker-socket-proxy). When the
// DOCKER_HOST hostname matches a running container — by name, compose service,
// or DNS alias — that shares a network with the current container, that shared
// network wins. SelectAutoNetworkMode's alphabetical heuristic routinely lands
// on an unrelated external network on multi-network setups, leaving the helper
// unable to reach the daemon (#3533).
func SelectDockerHostReachableNetworkMode(ctx context.Context, dockerClient *client.Client, inspect *container.InspectResponse, dockerHost string) string {
	fallback := SelectAutoNetworkMode(inspect)

	host := ""
	if parsed, err := url.Parse(strings.TrimSpace(dockerHost)); err == nil {
		host = parsed.Hostname()
	}
	if host == "" || dockerClient == nil || inspect == nil || inspect.NetworkSettings == nil || len(inspect.NetworkSettings.Networks) == 0 {
		return fallback
	}

	list, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		slog.DebugContext(ctx, "select docker-host network: list failed", "error", err)
		return fallback
	}

	shared := make([]string, 0, 2)
	for _, c := range list.Items {
		if c.NetworkSettings == nil || !containerAnswersToHostnameInternal(&c, host) {
			continue
		}
		for networkName := range c.NetworkSettings.Networks {
			if _, ok := inspect.NetworkSettings.Networks[networkName]; ok {
				shared = append(shared, networkName)
			}
		}
	}
	if len(shared) == 0 {
		return fallback
	}
	sort.Strings(shared)
	return shared[0]
}

// containerAnswersToHostnameInternal reports whether the container is
// addressable as host on some network: by container name, compose service
// name, or an endpoint DNS name/alias.
func containerAnswersToHostnameInternal(c *container.Summary, host string) bool {
	for _, name := range c.Names {
		if strings.TrimPrefix(name, "/") == host {
			return true
		}
	}
	if c.Labels["com.docker.compose.service"] == host {
		return true
	}
	for _, endpoint := range c.NetworkSettings.Networks {
		if endpoint == nil {
			continue
		}
		if slices.Contains(endpoint.DNSNames, host) || slices.Contains(endpoint.Aliases, host) {
			return true
		}
	}
	return false
}
