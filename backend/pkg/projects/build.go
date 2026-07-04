package projects

import (
	"fmt"
	"path/filepath"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	contextsource "go.getarcane.app/builds/pkg/utils/contextsource"
)

// ResolveBuildContext resolves a service build context against workingDir.
// The service config must have a non-nil Build field.
func ResolveBuildContext(workingDir string, svc composetypes.ServiceConfig, serviceName string) (string, error) {
	contextDir := strings.TrimSpace(svc.Build.Context)
	if contextDir == "" {
		contextDir = workingDir
	} else if _, isGitContext, err := contextsource.ParseGitBuildContextSource(contextDir); err != nil {
		return "", fmt.Errorf("invalid build context for service %s: %w", serviceName, err)
	} else if !isGitContext && !filepath.IsAbs(contextDir) {
		contextDir = filepath.Join(workingDir, contextDir)
	}

	if contextDir == "" {
		return "", fmt.Errorf("build context not set for service %s", serviceName)
	}

	return contextDir, nil
}

// ResolveDockerfilePath returns the configured Dockerfile path or Dockerfile.
// The service config must have a non-nil Build field.
func ResolveDockerfilePath(svc composetypes.ServiceConfig) string {
	dockerfilePath := strings.TrimSpace(svc.Build.Dockerfile)
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}

	return dockerfilePath
}
