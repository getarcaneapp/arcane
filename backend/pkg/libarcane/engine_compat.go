package libarcane

import (
	"context"
	"strings"

	containertypes "github.com/moby/moby/api/types/container"
	systemtypes "github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

// EngineCompatibilityInfo describes the container engine details Arcane uses to
// decide whether recreate-time HostConfig sanitization is required.
type EngineCompatibilityInfo struct {
	// Name is the normalized engine identifier, such as "docker" or "podman".
	Name string
	// CgroupVersion is the daemon-reported cgroup version, such as "1" or "2".
	CgroupVersion string
}

// PrepareRecreateHostConfigForEngine clones hostConfig and removes recreate
// options that are known to be incompatible with the connected engine.
//
// The returned HostConfig is a shallow copy, so field reassignment is isolated
// from the caller's original value, but content-level mutation of shared slice
// or map fields is not. The boolean result reports whether the helper removed
// any incompatible fields. EngineCompatibilityInfo reports the daemon details
// used to make that decision.
func PrepareRecreateHostConfigForEngine(ctx context.Context, dockerClient *client.Client, hostConfig *containertypes.HostConfig) (*containertypes.HostConfig, bool, EngineCompatibilityInfo, error) {
	if hostConfig == nil {
		return nil, false, EngineCompatibilityInfo{}, nil
	}

	cloned := cloneContainerHostConfigInternal(hostConfig)
	if dockerClient == nil {
		return cloned, false, EngineCompatibilityInfo{}, nil
	}

	serverVersion, err := dockerClient.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return cloned, false, EngineCompatibilityInfo{}, err
	}

	infoResult, err := dockerClient.Info(ctx, client.InfoOptions{})
	if err != nil {
		return cloned, false, EngineCompatibilityInfo{}, err
	}

	engineInfo := detectEngineCompatibilityInfoInternal(serverVersion, infoResult.Info)
	sanitized := sanitizeRecreateHostConfigInternal(cloned, engineInfo)

	return cloned, sanitized, engineInfo, nil
}

func cloneContainerHostConfigInternal(hostConfig *containertypes.HostConfig) *containertypes.HostConfig {
	if hostConfig == nil {
		return nil
	}

	cloned := *hostConfig
	return &cloned
}

func sanitizeRecreateHostConfigInternal(hostConfig *containertypes.HostConfig, engineInfo EngineCompatibilityInfo) bool {
	if hostConfig == nil {
		return false
	}

	if !isPodmanEngineInternal(engineInfo.Name) || !isCgroupV2Internal(engineInfo.CgroupVersion) {
		return false
	}

	if hostConfig.MemorySwappiness == nil {
		return false
	}

	hostConfig.MemorySwappiness = nil
	return true
}

func detectEngineCompatibilityInfoInternal(version client.ServerVersionResult, info systemtypes.Info) EngineCompatibilityInfo {
	return EngineCompatibilityInfo{
		Name:          detectEngineNameInternal(version, info),
		CgroupVersion: strings.TrimSpace(info.CgroupVersion),
	}
}

func detectEngineNameInternal(version client.ServerVersionResult, info systemtypes.Info) string {
	candidates := []string{version.Platform.Name}
	for _, component := range version.Components {
		candidates = append(candidates, component.Name)
		for _, value := range component.Details {
			candidates = append(candidates, value)
		}
	}
	candidates = append(candidates, info.ServerVersion, info.OperatingSystem)

	for _, candidate := range candidates {
		if name := normalizeEngineNameInternal(candidate); name != "" {
			return name
		}
	}

	return ""
}

func normalizeEngineNameInternal(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(normalized, "podman"):
		return "podman"
	case strings.Contains(normalized, "docker"):
		return "docker"
	default:
		return ""
	}
}

func isPodmanEngineInternal(engineName string) bool {
	return strings.EqualFold(strings.TrimSpace(engineName), "podman")
}

func isCgroupV2Internal(cgroupVersion string) bool {
	normalized := strings.ToLower(strings.TrimSpace(cgroupVersion))
	normalized = strings.TrimPrefix(normalized, "v")
	return normalized == "2"
}
