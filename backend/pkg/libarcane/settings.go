package libarcane

import (
	"strings"

	kit "go.getarcane.app/kit/pkg"
)

const (
	DepotTokenSettingKey = "depotToken"

	GHCRRegistryHost      = "ghcr.io"
	DockerHubRegistryHost = "docker.io"
	ECRPublicRegistryHost = "public.ecr.aws"
)

// ArcaneRegistryHost normalizes a registry setting to a host that mirrors the getarcaneapp images, defaulting to GHCR.
func ArcaneRegistryHost(registry string) string {
	return kit.Ternary(strings.TrimSpace(registry) == DockerHubRegistryHost, DockerHubRegistryHost, GHCRRegistryHost)
}

// ArcaneImageRegistryHost resolves the update-check registry setting; ECR Public only hosts the manager and agent images.
func ArcaneImageRegistryHost(registry string) string {
	if strings.TrimSpace(registry) == ECRPublicRegistryHost {
		return ECRPublicRegistryHost
	}
	return ArcaneRegistryHost(registry)
}

type SettingUpdate struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
