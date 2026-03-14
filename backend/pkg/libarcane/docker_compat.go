package libarcane

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const NetworkScopedMacAddressMinAPIVersion = "1.44"

// MultiEndpointContainerCreateMinAPIVersion is the first Docker API version
// that reliably accepts multiple entries in NetworkingConfig.EndpointsConfig on
// ContainerCreate.
//
// This matters because Arcane has several in-process container creation paths
// (auto-update recreate, manual create, self-upgrade recreate, embedded Compose
// library usage). Those paths all speak to the daemon through the Moby API
// client, so Arcane must handle daemon API compatibility itself.
//
// A host-side `docker compose up` command can still succeed against the same
// daemon because the installed Compose CLI/plugin may implement its own
// fallback strategy for older daemon APIs, such as creating the container on
// the primary network and then attaching the remaining networks with
// NetworkConnect. Arcane cannot assume the host CLI's fallback behavior exists
// in its own code paths, so the compatibility shim below applies that fallback
// explicitly.
const MultiEndpointContainerCreateMinAPIVersion = "1.44"

// DetectDockerAPIVersion returns the configured client API version when
// available, and falls back to the daemon-reported version only when the
// client version is not yet set.
func DetectDockerAPIVersion(ctx context.Context, dockerClient client.APIClient) string {
	if dockerClient == nil {
		return ""
	}

	if version := strings.TrimSpace(dockerClient.ClientVersion()); version != "" {
		return version
	}

	serverVersion, err := dockerClient.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return ""
	}

	return strings.TrimSpace(serverVersion.APIVersion)
}

// SupportsDockerCreateMultiEndpointNetworking reports whether the connected
// daemon API supports attaching multiple endpoints during ContainerCreate.
//
// On older daemon APIs, sending multiple EndpointsConfig entries can fail with
// daemon-side networking errors even though newer Compose CLIs may appear to
// "work" by using a different fallback sequence under the hood.
func SupportsDockerCreateMultiEndpointNetworking(apiVersion string) bool {
	return IsDockerAPIVersionAtLeast(apiVersion, MultiEndpointContainerCreateMinAPIVersion)
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
	cur, ok := parseAPIVersionInternal(current)
	if !ok {
		return false
	}

	minV, ok := parseAPIVersionInternal(minimum)
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
			endpointCopy.MacAddress = nil
		}

		cloned[networkName] = &endpointCopy
	}

	return cloned
}

// PrepareContainerCreateOptionsForDockerAPI rewrites a container create request
// for older daemon APIs that cannot accept multiple endpoint attachments in the
// initial ContainerCreate call.
//
// For API versions below MultiEndpointContainerCreateMinAPIVersion, Arcane
// keeps only the primary network in NetworkingConfig.EndpointsConfig and
// returns the remaining endpoints so the caller can attach them with
// NetworkConnect after create but before start. This mirrors the compatibility
// behavior that a newer host-side Compose CLI may apply internally, which
// explains why a shell `docker compose up` can succeed while Arcane's embedded
// or manual create paths fail unless we perform the split ourselves.
func PrepareContainerCreateOptionsForDockerAPI(options client.ContainerCreateOptions, apiVersion string) (client.ContainerCreateOptions, map[string]*network.EndpointSettings) {
	if SupportsDockerCreateMultiEndpointNetworking(apiVersion) || options.NetworkingConfig == nil || len(options.NetworkingConfig.EndpointsConfig) <= 1 {
		return options, nil
	}

	primaryNetwork := resolvePrimaryContainerCreateNetworkInternal(options.HostConfig, options.NetworkingConfig.EndpointsConfig)
	if primaryNetwork == "" {
		return options, nil
	}

	adjusted := options
	if options.HostConfig != nil {
		hostConfigCopy := *options.HostConfig
		adjusted.HostConfig = &hostConfigCopy
	}
	if adjusted.HostConfig == nil {
		adjusted.HostConfig = &container.HostConfig{}
	}
	if strings.TrimSpace(string(adjusted.HostConfig.NetworkMode)) == "" {
		adjusted.HostConfig.NetworkMode = container.NetworkMode(primaryNetwork)
	}

	primaryEndpoint := copyEndpointSettingsInternal(options.NetworkingConfig.EndpointsConfig[primaryNetwork])
	adjusted.NetworkingConfig = &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			primaryNetwork: primaryEndpoint,
		},
	}

	extraEndpoints := make(map[string]*network.EndpointSettings, len(options.NetworkingConfig.EndpointsConfig)-1)
	for networkName, endpoint := range options.NetworkingConfig.EndpointsConfig {
		if networkName == primaryNetwork {
			continue
		}
		extraEndpoints[networkName] = copyEndpointSettingsInternal(endpoint)
	}
	if len(extraEndpoints) == 0 {
		return adjusted, nil
	}

	return adjusted, extraEndpoints
}

// ConnectContainerExtraNetworksForDockerAPI attaches endpoints that were
// intentionally withheld from ContainerCreate for legacy daemon API
// compatibility.
//
// The intended call order is:
// 1. create the container on the primary network
// 2. connect each additional user-defined network
// 3. start the container
//
// Doing the attachment before start preserves the expected network topology
// while avoiding the legacy daemon limitation on multi-endpoint create.
func ConnectContainerExtraNetworksForDockerAPI(ctx context.Context, dockerClient client.APIClient, containerID string, endpoints map[string]*network.EndpointSettings) error {
	if dockerClient == nil || strings.TrimSpace(containerID) == "" || len(endpoints) == 0 {
		return nil
	}

	networkNames := make([]string, 0, len(endpoints))
	for networkName := range endpoints {
		networkNames = append(networkNames, networkName)
	}
	slices.Sort(networkNames)

	for _, networkName := range networkNames {
		_, err := dockerClient.NetworkConnect(ctx, networkName, client.NetworkConnectOptions{
			Container:      containerID,
			EndpointConfig: copyEndpointSettingsInternal(endpoints[networkName]),
		})
		if err != nil {
			return fmt.Errorf("connect network %s: %w", networkName, err)
		}
	}

	return nil
}

// ContainerCreateWithCompatibility applies Arcane's Docker API compatibility
// shims before calling ContainerCreate.
//
// This helper exists so every in-process Arcane create/recreate path can share
// the same daemon-compatibility behavior instead of relying on whichever
// fallback logic a separate host CLI binary might have. In practice this is the
// guardrail that keeps Arcane aligned with older daemons such as Docker 24.x
// advertising API 1.43 when a container needs multiple user-defined networks.
func ContainerCreateWithCompatibility(ctx context.Context, dockerClient client.APIClient, options client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	return ContainerCreateWithCompatibilityForAPIVersion(ctx, dockerClient, options, DetectDockerAPIVersion(ctx, dockerClient))
}

// ContainerCreateWithCompatibilityForAPIVersion applies Arcane's Docker API
// compatibility shims before calling ContainerCreate when the caller has
// already resolved the daemon API version.
func ContainerCreateWithCompatibilityForAPIVersion(ctx context.Context, dockerClient client.APIClient, options client.ContainerCreateOptions, apiVersion string) (client.ContainerCreateResult, error) {
	if dockerClient == nil {
		return client.ContainerCreateResult{}, fmt.Errorf("docker api client is nil")
	}

	adjustedOptions, extraEndpoints := PrepareContainerCreateOptionsForDockerAPI(options, apiVersion)

	result, err := dockerClient.ContainerCreate(ctx, adjustedOptions)
	if err != nil {
		return client.ContainerCreateResult{}, err
	}

	if len(extraEndpoints) == 0 {
		return result, nil
	}

	if err := ConnectContainerExtraNetworksForDockerAPI(ctx, dockerClient, result.ID, extraEndpoints); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, result.ID, client.ContainerRemoveOptions{Force: true})
		return client.ContainerCreateResult{}, err
	}

	return result, nil
}

func parseAPIVersionInternal(version string) ([3]int, bool) {
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

func resolvePrimaryContainerCreateNetworkInternal(hostConfig *container.HostConfig, endpoints map[string]*network.EndpointSettings) string {
	if len(endpoints) == 0 {
		return ""
	}

	if hostConfig != nil {
		networkMode := strings.TrimSpace(string(hostConfig.NetworkMode))
		switch {
		case networkMode == "":
		case container.NetworkMode(networkMode).IsHost(),
			container.NetworkMode(networkMode).IsNone(),
			container.NetworkMode(networkMode).IsContainer():
			return ""
		default:
			if _, ok := endpoints[networkMode]; ok {
				return networkMode
			}
			// Named network set in NetworkMode is not represented in endpoint
			// config, so avoid splitting the request and let the daemon handle
			// the original create options as-is.
			return ""
		}
	}

	networkNames := make([]string, 0, len(endpoints))
	for networkName := range endpoints {
		networkNames = append(networkNames, networkName)
	}
	slices.Sort(networkNames)
	return networkNames[0]
}

func copyEndpointSettingsInternal(endpoint *network.EndpointSettings) *network.EndpointSettings {
	if endpoint == nil {
		return nil
	}

	return endpoint.Copy()
}
