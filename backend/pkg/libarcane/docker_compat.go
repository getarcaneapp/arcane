package libarcane

import (
	"context"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const NetworkScopedMacAddressMinAPIVersion = "1.44"

// DetectDockerAPIVersion returns the negotiated client API version when
// available, and falls back to querying the daemon server version.
func DetectDockerAPIVersion(ctx context.Context, dockerClient *client.Client) string {
	if dockerClient == nil {
		return ""
	}

	if version := strings.TrimSpace(dockerClient.ClientVersion()); version != "" {
		return version
	}

	serverVersion, err := dockerClient.ServerVersion(ctx)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(serverVersion.APIVersion)
}

// SupportsDockerCreatePerNetworkMACAddress reports whether the daemon API
// supports per-network mac-address on container create (Docker API >= 1.44).
func SupportsDockerCreatePerNetworkMACAddress(apiVersion string) bool {
	return IsDockerAPIVersionAtLeast(apiVersion, NetworkScopedMacAddressMinAPIVersion)
}

// IsDockerAPIVersionAtLeast performs numeric dot-segment comparison for Docker
// API versions (e.g. "1.43", "1.44.1"). Returns false when either version
// cannot be parsed.
func IsDockerAPIVersionAtLeast(current, minimum string) bool {
	cur, ok := parseAPIVersion(current)
	if !ok {
		return false
	}

	minV, ok := parseAPIVersion(minimum)
	if !ok {
		return false
	}

	for i := range len(cur) {
		if cur[i] > minV[i] {
			return true
		}
		if cur[i] < minV[i] {
			return false
		}
	}

	return true
}

// SanitizeContainerCreateEndpointSettingsForDockerAPI clones endpoint settings
// for container recreate and removes per-network mac-address when daemon API
// does not support it (API < 1.44).
func SanitizeContainerCreateEndpointSettingsForDockerAPI(endpoints map[string]*network.EndpointSettings, apiVersion string) map[string]*network.EndpointSettings {
	if len(endpoints) == 0 {
		return nil
	}

	keepPerNetworkMAC := SupportsDockerCreatePerNetworkMACAddress(apiVersion)
	cloned := make(map[string]*network.EndpointSettings, len(endpoints))

	for networkName, endpoint := range endpoints {
		if endpoint == nil {
			cloned[networkName] = nil
			continue
		}

		endpointCopy := *endpoint
		if !keepPerNetworkMAC {
			endpointCopy.MacAddress = ""
		}

		cloned[networkName] = &endpointCopy
	}

	return cloned
}

func parseAPIVersion(version string) ([3]int, bool) {
	parsed := [3]int{}

	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	if version == "" {
		return parsed, false
	}

	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return parsed, false
	}

	for i := 0; i < len(parsed) && i < len(parts); i++ {
		part := strings.TrimSpace(parts[i])
		if part == "" {
			return [3]int{}, false
		}

		n, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}, false
		}
		parsed[i] = n
	}

	return parsed, true
}
