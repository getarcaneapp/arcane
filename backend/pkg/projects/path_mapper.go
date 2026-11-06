package projects

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/samber/mo"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/pkg/selfcontainer"
	"getarcane.dev/types/v3"
)

type (
	HostMount struct {
		Destination string // container-side mount path, e.g. "/app/data/projects/homeassistant"
		Source      string // host-side path, e.g. "/home/user/homeassistant"
	}

	PathMapper struct {
		containerPrefix string      // e.g., "/app/data/projects" (single-prefix mode)
		hostPrefix      string      // e.g., "D:/self-hosted/arcane/projects" (single-prefix mode)
		mounts          []HostMount // when set, sources resolve by longest-prefix match (auto-discovery mode)
		isNonMatching   bool
	}

	remapTarget struct {
		apply   func(hostPath string)
		kind    string
		name    string
		key     string
		current string
	}
)

func NewPathMapper(containerDir, hostDir string) *PathMapper {
	container := filepath.Clean(containerDir)
	host := hostDir
	if host == "" {
		host = container
	}
	host = filepath.Clean(host)

	return &PathMapper{
		containerPrefix: container,
		hostPrefix:      host,
		isNonMatching:   container != host,
	}
}

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

func NewPathMapperForConfiguredDirectory(ctx context.Context, configuredPath, defaultDir string, dockerClient *client.Client) *PathMapper {
	configuredPath = cmp.Or(strings.TrimSpace(configuredPath), defaultDir)

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
		return kit.Ternary(pathMapper.isNonMatching, pathMapper, nil)
	}

	mounts, err := GetCurrentContainerMounts(ctx, dockerClient)
	if err != nil || len(mounts) == 0 {
		return nil
	}

	pathMapper := NewPathMapperFromMounts(mounts)
	return kit.Ternary(pathMapper.isNonMatching, pathMapper, nil)
}

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
		host = joinHostRelativePath(host, bestRel)
	}
	return mo.Some(host)
}

func joinHostRelativePath(base, rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	if rel == "" || rel == "." {
		return base
	}

	useBackslash := IsWindowsDrivePath(base) && strings.Contains(base, "\\")
	normalized := strings.ReplaceAll(base, "\\", "/")

	drive := ""
	if IsWindowsDrivePath(normalized) {
		drive, normalized = normalized[:2], normalized[2:]
	}

	joined := drive + path.Join(normalized, rel)
	if useBackslash {
		joined = strings.ReplaceAll(joined, "/", "\\")
	}
	return joined
}

func (pm *PathMapper) ContainerToHost(containerPath string) (string, bool, error) {
	// Auto-discovery mode: resolve against Arcane's real mount table so nested,
	// independently bind-mounted directories map to their own host path.
	if len(pm.mounts) > 0 {
		if host, ok := ResolveHostPath(pm.mounts, containerPath).Get(); ok {
			return host, true, nil
		}
		return filepath.Clean(containerPath), false, nil
	}

	cleaned := filepath.Clean(containerPath)

	relPath, err := filepath.Rel(pm.containerPrefix, cleaned)
	if err != nil {
		return "", false, fmt.Errorf("failed to calculate relative path: %w", err)
	}

	if strings.HasPrefix(relPath, "..") || relPath == ".." || filepath.IsAbs(relPath) {
		return cleaned, false, nil
	}

	hostPath := filepath.Join(pm.hostPrefix, relPath)

	if strings.Contains(pm.hostPrefix, ":") || strings.HasPrefix(pm.hostPrefix, "\\") {
		hostPath = filepath.ToSlash(hostPath)
	}

	return hostPath, true, nil
}

func (pm *PathMapper) TranslateVolumeSources(project *composetypes.Project, translateFileResources bool) error {
	if !pm.isNonMatching {
		return nil
	}

	for si := range project.Services {
		service := project.Services[si]
		for vi := range service.Volumes {
			volume := service.Volumes[vi]

			if volume.Type != composetypes.VolumeTypeBind {
				continue
			}

			hostPath, _, err := pm.ContainerToHost(volume.Source)
			if err != nil {
				return fmt.Errorf("failed to translate volume source %q: %w", volume.Source, err)
			}

			volume.Source = hostPath
			service.Volumes[vi] = volume
		}
		project.Services[si] = service
	}

	if !translateFileResources {
		return nil
	}

	for name, secret := range project.Secrets {
		if secret.File != "" {
			hostPath, _, err := pm.ContainerToHost(secret.File)
			if err != nil {
				return fmt.Errorf("failed to translate secret file %q: %w", secret.File, err)
			}
			secret.File = hostPath
			project.Secrets[name] = secret
		}
	}

	for name, config := range project.Configs {
		if config.File != "" {
			hostPath, _, err := pm.ContainerToHost(config.File)
			if err != nil {
				return fmt.Errorf("failed to translate config file %q: %w", config.File, err)
			}
			config.File = hostPath
			project.Configs[name] = config
		}
	}

	for name, volume := range project.Volumes {
		device, ok := bindVolumeDevice(volume)
		if !ok || (!filepath.IsAbs(device) && !IsWindowsDrivePath(device)) {
			continue
		}

		hostPath, _, err := pm.ContainerToHost(device)
		if err != nil {
			return fmt.Errorf("failed to translate volume device %q: %w", device, err)
		}
		volume.DriverOpts["device"] = hostPath
		project.Volumes[name] = volume
	}

	return nil
}

func bindVolumeDevice(volume composetypes.VolumeConfig) (string, bool) {
	if volume.Driver != "" && volume.Driver != "local" {
		return "", false
	}
	if !strings.Contains(volume.DriverOpts["o"], "bind") {
		return "", false
	}

	device := strings.TrimSpace(volume.DriverOpts["device"])
	return device, device != ""
}

func RemapEscapedRelativeSources(
	ctx context.Context,
	pathMapper types.VolumeSourcePathMapper,
	project *composetypes.Project,
	containerWorkingDir string,
	rawSources map[string]string,
	includeFileResources bool,
) {
	if project == nil || pathMapper == nil || len(rawSources) == 0 || containerWorkingDir == "" {
		return
	}

	hostWorkingDir := "" // resolved on first use, so a project with nothing to remap logs nothing
	for _, target := range remapTargets(project, includeFileResources) {
		// A missing key yields "", which is not remappable, so absent raw sources
		// fall out here along with absolute ones.
		rawSource := rawSources[target.key]
		if !isRemappableSource(rawSource) {
			continue
		}
		if target.current != filepath.Clean(filepath.Join(containerWorkingDir, rawSource)) {
			continue // already translated through the mount table
		}

		if hostWorkingDir == "" {
			resolved, ok := resolveHostWorkingDir(ctx, pathMapper, containerWorkingDir)
			if !ok {
				return // without a host project directory nothing can be anchored
			}
			hostWorkingDir = resolved
		}

		hostPath := joinHostRelativePath(hostWorkingDir, rawSource)
		if hostPath == target.current {
			continue // identity mount: the Docker host already resolves it the same way
		}
		target.apply(hostPath)
		slog.WarnContext(ctx,
			"compose relative path resolves outside the mounted projects directory; remapped so the Docker host resolves it the same way `docker compose up` would",
			"kind", target.kind, "name", target.name, "spec", rawSource,
			"containerPath", target.current, "hostPath", hostPath,
		)
	}
}

func remapTargets(project *composetypes.Project, includeFileResources bool) []remapTarget {
	var targets []remapTarget

	for name, service := range project.Services {
		for i := range service.Volumes {
			// Mutating through the pointer is enough: the slice shares its backing
			// array with the ServiceConfig held in project.Services.
			volume := &service.Volumes[i]
			if volume.Type != composetypes.VolumeTypeBind {
				continue
			}
			targets = append(targets, remapTarget{
				kind:    "service_volume",
				name:    name,
				key:     ("volume:" + name + "\x00" + filepath.Clean(volume.Target)),
				current: volume.Source,
				apply:   func(hostPath string) { volume.Source = hostPath },
			})
		}
	}

	for name, volume := range project.Volumes {
		device, ok := bindVolumeDevice(volume)
		if !ok {
			continue
		}
		targets = append(targets, remapTarget{
			kind:    "volume_device",
			name:    name,
			key:     "volume_device:" + name,
			current: device,
			apply:   func(hostPath string) { volume.DriverOpts["device"] = hostPath },
		})
	}

	if !includeFileResources {
		return targets
	}

	for name, secret := range project.Secrets {
		targets = append(targets, remapTarget{
			kind:    "secret",
			name:    name,
			key:     "secret:" + name,
			current: secret.File,
			apply:   func(hostPath string) { secret.File = hostPath; project.Secrets[name] = secret },
		})
	}

	for name, config := range project.Configs {
		targets = append(targets, remapTarget{
			kind:    "config",
			name:    name,
			key:     "config:" + name,
			current: config.File,
			apply:   func(hostPath string) { config.File = hostPath; project.Configs[name] = config },
		})
	}

	return targets
}

func isRemappableSource(rawSource string) bool {
	rawSource = strings.TrimSpace(rawSource)
	if rawSource == "" || strings.HasPrefix(rawSource, "~") || strings.HasPrefix(rawSource, `\\`) {
		return false
	}
	return !filepath.IsAbs(rawSource) && !IsWindowsDrivePath(rawSource)
}

func resolveHostWorkingDir(ctx context.Context, pathMapper types.VolumeSourcePathMapper, containerWorkingDir string) (string, bool) {
	hostWorkingDir, mapped, err := pathMapper.ContainerToHost(containerWorkingDir)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve host path for project directory; relative paths outside the projects mount may resolve incorrectly",
			"workingDir", containerWorkingDir, "error", err)
		return "", false
	}

	if !mapped {
		slog.WarnContext(ctx, "project directory is not inside a mounted directory; relative paths outside the projects mount may resolve incorrectly",
			"workingDir", containerWorkingDir)
		return "", false
	}

	return hostWorkingDir, true
}

func IsWindowsDrivePath(candidate string) bool {
	if len(candidate) < 3 {
		return false
	}
	return ((candidate[0] >= 'a' && candidate[0] <= 'z') || (candidate[0] >= 'A' && candidate[0] <= 'Z')) &&
		candidate[1] == ':' &&
		(candidate[2] == '/' || candidate[2] == '\\')
}

func GetCurrentContainerMounts(ctx context.Context, dockerCli *client.Client) ([]HostMount, error) {
	if dockerCli == nil {
		return nil, nil
	}

	inspect, err := selfcontainer.InspectCurrentArcaneContainer(ctx, dockerCli)
	if err != nil {
		return nil, err
	}

	mounts := make([]HostMount, 0, len(inspect.Mounts))
	for i := range inspect.Mounts {
		m := &inspect.Mounts[i]
		if m.Type != mounttypes.TypeBind && m.Type != mounttypes.TypeVolume {
			continue
		}
		if strings.TrimSpace(m.Source) == "" || strings.TrimSpace(m.Destination) == "" {
			continue
		}
		mounts = append(mounts, HostMount{Destination: m.Destination, Source: m.Source})
	}
	return mounts, nil
}

func GetHostPathForContainerPath(ctx context.Context, dockerCli *client.Client, containerPath string) (string, error) {
	mounts, err := GetCurrentContainerMounts(ctx, dockerCli)
	if err != nil {
		return "", err
	}

	if host, ok := ResolveHostPath(mounts, containerPath).Get(); ok {
		return host, nil
	}

	return "", nil
}
