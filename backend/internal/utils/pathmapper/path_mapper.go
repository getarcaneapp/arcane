package pathmapper

import (
	"fmt"
	"path/filepath"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// PathMapper handles translation between container and host paths
type PathMapper struct {
	containerPrefix string // e.g., "/app/data/projects"
	hostPrefix      string // e.g., "D:/self-hosted/arcane/projects"
	isNonMatching   bool   // true if paths differ
}

// NewPathMapper creates a new path mapper
func NewPathMapper(containerDir, hostDir string) *PathMapper {
	container := filepath.Clean(containerDir)
	host := hostDir
	if host == "" {
		host = container // Matching mount (Linux/macOS)
	}
	host = filepath.Clean(host)

	return &PathMapper{
		containerPrefix: container,
		hostPrefix:      host,
		isNonMatching:   container != host,
	}
}

// ContainerToHost translates a container path to host path
func (pm *PathMapper) ContainerToHost(containerPath string) (string, error) {
	if !pm.isNonMatching {
		return containerPath, nil // No translation needed
	}

	cleaned := filepath.Clean(containerPath)

	// If containerPath is absolute and containerPrefix is not, or vice versa,
	// we cannot safely determine if the path is within our prefix.
	// Return the path unchanged as it's outside our scope.
	cleanedAbs := filepath.IsAbs(cleaned)
	prefixAbs := filepath.IsAbs(pm.containerPrefix)
	if cleanedAbs != prefixAbs {
		return cleaned, nil
	}

	// If both are absolute, check if cleaned is under containerPrefix
	if cleanedAbs && prefixAbs {
		if !strings.HasPrefix(cleaned, pm.containerPrefix) {
			// Path is absolute but outside our container prefix - no translation
			return cleaned, nil
		}
	}

	// Calculate relative path
	relPath, err := filepath.Rel(pm.containerPrefix, cleaned)
	if err != nil {
		// This should not happen now, but keep as safety net
		return cleaned, nil
	}

	// Only translate paths within container prefix
	if strings.HasPrefix(relPath, "..") || relPath == ".." || filepath.IsAbs(relPath) {
		return cleaned, nil
	}

	// Join with host prefix
	hostPath := filepath.Join(pm.hostPrefix, relPath)

	// Force forward slashes if host looks like a Windows path but we're on Linux
	// Docker on Windows accepts forward slashes fine
	if strings.Contains(pm.hostPrefix, ":") || strings.HasPrefix(pm.hostPrefix, "\\") {
		hostPath = filepath.ToSlash(hostPath)
	}

	return hostPath, nil
}

// TranslateVolumeSources translates all bind mount sources in a compose project
func (pm *PathMapper) TranslateVolumeSources(project *composetypes.Project) error {
	if !pm.isNonMatching {
		return nil // No translation needed
	}

	// Translate service volumes
	for si := range project.Services {
		service := project.Services[si]
		for vi := range service.Volumes {
			volume := service.Volumes[vi]

			// Only translate bind mounts
			if volume.Type != composetypes.VolumeTypeBind {
				continue
			}

			hostPath, err := pm.ContainerToHost(volume.Source)
			if err != nil {
				return fmt.Errorf("failed to translate volume source %q: %w", volume.Source, err)
			}

			volume.Source = hostPath
			service.Volumes[vi] = volume
		}
		project.Services[si] = service
	}

	// Translate secrets
	for name, secret := range project.Secrets {
		if secret.File != "" {
			hostPath, err := pm.ContainerToHost(secret.File)
			if err != nil {
				return fmt.Errorf("failed to translate secret file %q: %w", secret.File, err)
			}
			secret.File = hostPath
			project.Secrets[name] = secret
		}
	}

	// Translate configs
	for name, config := range project.Configs {
		if config.File != "" {
			hostPath, err := pm.ContainerToHost(config.File)
			if err != nil {
				return fmt.Errorf("failed to translate config file %q: %w", config.File, err)
			}
			config.File = hostPath
			project.Configs[name] = config
		}
	}

	return nil
}

func (pm *PathMapper) IsNonMatchingMount() bool {
	return pm.isNonMatching
}

// IsWindowsDrivePath returns true if the path looks like a Windows drive path (e.g., "C:/path")
func IsWindowsDrivePath(path string) bool {
	if len(path) < 3 {
		return false
	}
	return ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) &&
		path[1] == ':' &&
		(path[2] == '/' || path[2] == '\\')
}
