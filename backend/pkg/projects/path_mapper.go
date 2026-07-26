package projects

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"emperror.dev/errors"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
)

// HostMount is one container→host mount (bind or named volume) from Arcane's own
// container. It is used for longest-prefix host-path resolution in Docker-in-Docker
// setups, where independently bind-mounted project directories each map to their own
// host path rather than a single projects-root prefix.
type HostMount struct {
	Destination string // container-side mount path, e.g. "/app/data/projects/homeassistant"
	Source      string // host-side path, e.g. "/home/user/homeassistant"
}

// PathMapper handles translation between container and host paths
type PathMapper struct {
	containerPrefix string      // e.g., "/app/data/projects" (single-prefix mode)
	hostPrefix      string      // e.g., "D:/self-hosted/arcane/projects" (single-prefix mode)
	isNonMatching   bool        // true if a translation can occur
	mounts          []HostMount // when set, sources resolve by longest-prefix match (auto-discovery mode)
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

// NewPathMapperFromMounts creates a path mapper that resolves each source against the
// given container mount table by longest-prefix match, instead of a single
// container→host prefix. This is used for Docker-in-Docker auto-discovery so that an
// independently bind-mounted project directory maps to its real host path.
func NewPathMapperFromMounts(mounts []HostMount) *PathMapper {
	nonMatching := false
	for i := range mounts {
		if filepath.Clean(mounts[i].Source) != filepath.Clean(mounts[i].Destination) {
			nonMatching = true
			break
		}
	}

	return &PathMapper{
		mounts:        mounts,
		isNonMatching: nonMatching,
	}
}

// NewPathMapperForConfiguredDirectory resolves an optional container-to-host
// directory mapping and falls back to Docker mount discovery when no explicit
// host path is configured.
func NewPathMapperForConfiguredDirectory(ctx context.Context, configuredPath, defaultDir string, dockerClient *client.Client) *PathMapper {
	configuredPath = strings.TrimSpace(configuredPath)
	if configuredPath == "" {
		configuredPath = defaultDir
	}

	containerDir := configuredPath
	hostDir := ""
	if parts := strings.SplitN(configuredPath, ":", 2); len(parts) == 2 &&
		!IsWindowsDrivePath(configuredPath) &&
		strings.HasPrefix(parts[0], "/") {
		containerDir = parts[0]
		hostDir = parts[1]
	}

	containerDir, err := GetProjectsDirectory(ctx, strings.TrimSpace(containerDir))
	if err != nil {
		slog.WarnContext(ctx, "unable to resolve configured source directory, using default", "path", configuredPath, "error", err)
		containerDir = filepath.Clean(defaultDir)
	}

	if strings.TrimSpace(hostDir) != "" {
		pathMapper := NewPathMapper(containerDir, filepath.Clean(strings.TrimSpace(hostDir)))
		if pathMapper.IsNonMatchingMount() {
			return pathMapper
		}
		return nil
	}

	mounts, err := GetCurrentContainerMounts(ctx, dockerClient)
	if err != nil || len(mounts) == 0 {
		return nil
	}

	pathMapper := NewPathMapperFromMounts(mounts)
	if pathMapper.IsNonMatchingMount() {
		return pathMapper
	}
	return nil
}

// ResolveHostPath returns the host-side path for containerPath by selecting the
// longest-prefix mount whose Destination contains it and appending the trailing relative
// segment to that mount's Source. It returns None when no mount contains the path.
func ResolveHostPath(mounts []HostMount, containerPath string) mo.Option[string] {
	cleaned := filepath.Clean(containerPath)

	var (
		bestSource string
		bestRel    string
		bestLen    = -1
	)
	for i := range mounts {
		dest := filepath.Clean(mounts[i].Destination)
		rel, err := filepath.Rel(dest, cleaned)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		if len(dest) > bestLen {
			bestLen = len(dest)
			bestSource = mounts[i].Source
			bestRel = rel
		}
	}
	if bestLen < 0 {
		return mo.None[string]()
	}

	host := bestSource
	if bestRel != "." {
		// Mirror the separator handling used for Windows hosts: keep backslashes when the
		// host source is a Windows drive path, otherwise use forward slashes.
		separator := "/"
		if IsWindowsDrivePath(host) && strings.Contains(host, "\\") {
			separator = "\\"
			bestRel = strings.ReplaceAll(bestRel, "/", "\\")
		}
		if !strings.HasSuffix(host, separator) {
			host += separator
		}
		host += bestRel
	}
	return mo.Some(host)
}

// ContainerToHost translates a container path to host path
func (pm *PathMapper) ContainerToHost(containerPath string) (string, error) {
	if !pm.IsNonMatchingMount() {
		return containerPath, nil // No translation needed
	}

	// Auto-discovery mode: resolve against Arcane's real mount table so nested,
	// independently bind-mounted directories map to their own host path.
	if len(pm.mounts) > 0 {
		if host, ok := ResolveHostPath(pm.mounts, containerPath).Get(); ok {
			return host, nil
		}
		return filepath.Clean(containerPath), nil // outside all mounts: leave unchanged
	}

	cleaned := filepath.Clean(containerPath)

	// Calculate relative path
	relPath, err := filepath.Rel(pm.containerPrefix, cleaned)
	if err != nil {
		return "", errors.WrapIf(err, "failed to calculate relative path")
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

// TranslateVolumeSources translates bind mount sources in a Compose project.
// File-backed configs and secrets are translated only when the Docker host,
// rather than Arcane itself, will read those files.
func (pm *PathMapper) TranslateVolumeSources(project *composetypes.Project, translateFileResources bool) error {
	if !pm.IsNonMatchingMount() {
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
				return errors.WrapIff(err, "failed to translate volume source %q", volume.Source)
			}

			volume.Source = hostPath
			service.Volumes[vi] = volume
		}
		project.Services[si] = service
	}

	if !translateFileResources {
		return nil
	}

	// Translate secrets
	for name, secret := range project.Secrets {
		if secret.File != "" {
			hostPath, err := pm.ContainerToHost(secret.File)
			if err != nil {
				return errors.WrapIff(err, "failed to translate secret file %q", secret.File)
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
				return errors.WrapIff(err, "failed to translate config file %q", config.File)
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
