package registry

import (
	"strings"

	"kit.getarcane.app"
)

const (
	GHCRRegistryHost      = "ghcr.io"
	ECRPublicRegistryHost = "public.ecr.aws"
)

func ArcaneRegistryHost(registry string) string {
	return kit.Ternary(strings.TrimSpace(registry) == DefaultRegistryDomain, DefaultRegistryDomain, GHCRRegistryHost)
}

func ArcaneImageRegistryHost(registry string) string {
	if strings.TrimSpace(registry) == ECRPublicRegistryHost {
		return ECRPublicRegistryHost
	}
	return ArcaneRegistryHost(registry)
}
