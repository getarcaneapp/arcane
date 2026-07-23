package projects

import (
	"path/filepath"
	"strings"

	"emperror.dev/errors"
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
		return "", errors.WrapIff(err, "invalid build context for service %s", serviceName)
	} else if !isGitContext && !filepath.IsAbs(contextDir) {
		contextDir = filepath.Join(workingDir, contextDir)
	}

	if contextDir == "" {
		return "", errors.Errorf("build context not set for service %s", serviceName)
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
