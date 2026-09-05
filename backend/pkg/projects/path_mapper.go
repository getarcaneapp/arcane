package projects

import (
	"context"
	"log/slog"
	"path"
	"path/filepath"
	"strings"

	"emperror.dev/errors"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	mounttypes "github.com/moby/moby/api/types/mount"
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
		host = joinHostRelativePathInternal(host, bestRel)
	}
	return mo.Some(host)
}

// joinHostRelativePathInternal joins rel onto a host base path and cleans the
// result. A Windows drive prefix is held aside while cleaning so that ".."
// segments cannot consume it, and the base's separator style is preserved:
// backslashes are kept for a Windows drive path written with them, forward
// slashes otherwise (Docker on Windows accepts forward slashes fine).
func joinHostRelativePathInternal(base, rel string) string {
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

	// Translate bind-backed named volumes. The Docker daemon, not Arcane, opens
	// the device path, so it needs the same translation as a bind mount source.
	for name, volume := range project.Volumes {
		device, ok := bindVolumeDeviceInternal(volume)
		// Only absolute devices can be translated. compose-go absolutizes a
		// relative device only when `o` is exactly "bind", so one that is still
		// relative here keeps its existing behavior instead of failing the load.
		if !ok || (!filepath.IsAbs(device) && !IsWindowsDrivePath(device)) {
			continue
		}

		hostPath, err := pm.ContainerToHost(device)
		if err != nil {
			return errors.WrapIff(err, "failed to translate volume device %q", device)
		}
		volume.DriverOpts["device"] = hostPath
		project.Volumes[name] = volume
	}

	return nil
}

// bindVolumeDeviceInternal returns the device path of a bind-backed named volume
// (local driver with `o: bind`), which compose-go resolves against the working
// directory just like a bind mount source.
//
// The check is deliberately looser than compose-go's: an empty driver counts as
// local because that is how Docker defaults it, and `o` only has to contain
// "bind" rather than equal it, since a device is a host path whether or not
// compose-go chose to resolve it.
func bindVolumeDeviceInternal(volume composetypes.VolumeConfig) (string, bool) {
	if volume.Driver != "" && volume.Driver != "local" {
		return "", false
	}
	if !strings.Contains(volume.DriverOpts["o"], "bind") {
		return "", false
	}

	device := strings.TrimSpace(volume.DriverOpts["device"])
	return device, device != ""
}

func (pm *PathMapper) IsNonMatchingMount() bool {
	return pm.isNonMatching
}

// VolumeSourceKey identifies a service bind mount across two loads of the same
// Compose input. Bind targets are unique per service, so the target is a stable
// key even though the source is exactly what differs between the loads.
func VolumeSourceKey(service, target string) string {
	return "volume:" + service + "\x00" + filepath.Clean(target)
}

// RemapEscapedRelativeSources rewrites daemon-side paths that compose-go
// resolved against the container-side project directory but that
// TranslateVolumeSources could not map, because they escaped every mounted
// directory.
//
// compose-go absolutizes relative paths against ConfigDetails.WorkingDir, which
// for Arcane is a container path. A source like `../../data` can therefore land
// outside the projects mount, where prefix translation has nothing to match and
// leaves the path untouched — so the daemon creates it at the wrong place on the
// host, unlike `docker compose up` run in the project directory. Relativity is
// not recoverable from the resolved path (an intentionally absolute source is an
// identical string by then), so the raw pre-resolution paths are passed in as
// rawSources, keyed by VolumeSourceKey and by "<kind>:<name>".
//
// Anything TranslateVolumeSources already rewrote is left alone, so nested
// independently-bind-mounted directories keep resolving through the mount table.
func RemapEscapedRelativeSources(
	ctx context.Context,
	pathMapper projecttypes.VolumeSourcePathMapper,
	project *composetypes.Project,
	containerWorkingDir string,
	rawSources map[string]string,
	includeFileResources bool,
) {
	if project == nil || pathMapper == nil || len(rawSources) == 0 || containerWorkingDir == "" {
		return
	}

	hostWorkingDir := "" // resolved on first use, so a project with nothing to remap logs nothing
	for _, target := range remapTargetsInternal(project, includeFileResources) {
		// A missing key yields "", which is not remappable, so absent raw sources
		// fall out here along with absolute ones.
		rawSource := rawSources[target.key]
		if !isRemappableSourceInternal(rawSource) {
			continue
		}
		if target.current != filepath.Clean(filepath.Join(containerWorkingDir, rawSource)) {
			continue // already translated through the mount table
		}

		if hostWorkingDir == "" {
			resolved, ok := hostWorkingDirInternal(ctx, pathMapper, containerWorkingDir)
			if !ok {
				return // without a host project directory nothing can be anchored
			}
			hostWorkingDir = resolved
		}

		hostPath := joinHostRelativePathInternal(hostWorkingDir, rawSource)
		target.apply(hostPath)
		slog.WarnContext(ctx,
			"compose relative path resolves outside the mounted projects directory; remapped so the Docker host resolves it the same way `docker compose up` would",
			"kind", target.kind, "name", target.name, "spec", rawSource,
			"container_path", target.current, "host_path", hostPath,
		)
	}
}

// remapTarget is one daemon-side path that may need re-resolving, paired with how
// to write the new value back into the project.
type remapTarget struct {
	kind    string
	name    string
	key     string
	current string
	apply   func(hostPath string)
}

// remapTargetsInternal collects every daemon-side path in the project, so that
// deciding which ones to rewrite stays a single flat pass. File-backed secrets
// and configs are included only when the Docker host, rather than Arcane itself,
// reads them — matching TranslateVolumeSources.
func remapTargetsInternal(project *composetypes.Project, includeFileResources bool) []remapTarget {
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
				key:     VolumeSourceKey(name, volume.Target),
				current: volume.Source,
				apply:   func(hostPath string) { volume.Source = hostPath },
			})
		}
	}

	for name, volume := range project.Volumes {
		device, ok := bindVolumeDeviceInternal(volume)
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

// isRemappableSourceInternal reports whether a raw Compose path can be
// re-resolved against a different working directory. Absolute paths (including
// Windows drive and UNC paths, which compose-go also leaves alone) are already
// meaningful to the Docker host, and `~` expands against the container's home
// directory, which has no host equivalent — all keep their current behavior.
func isRemappableSourceInternal(rawSource string) bool {
	rawSource = strings.TrimSpace(rawSource)
	if rawSource == "" || strings.HasPrefix(rawSource, "~") || strings.HasPrefix(rawSource, `\\`) {
		return false
	}
	return !filepath.IsAbs(rawSource) && !IsWindowsDrivePath(rawSource)
}

// hostWorkingDirInternal resolves the host-side project directory that relative
// paths must be re-resolved against. It reports false when the project directory
// itself is not inside a mounted directory, since there is then no host path to
// anchor to.
func hostWorkingDirInternal(ctx context.Context, pathMapper projecttypes.VolumeSourcePathMapper, containerWorkingDir string) (string, bool) {
	hostWorkingDir, err := pathMapper.ContainerToHost(containerWorkingDir)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve host path for project directory; relative paths outside the projects mount may resolve incorrectly",
			"working_dir", containerWorkingDir, "error", err)
		return "", false
	}

	if hostWorkingDir == "" || filepath.Clean(hostWorkingDir) == filepath.Clean(containerWorkingDir) {
		slog.WarnContext(ctx, "project directory is not inside a mounted directory; relative paths outside the projects mount may resolve incorrectly",
			"working_dir", containerWorkingDir)
		return "", false
	}

	return hostWorkingDir, true
}

// IsWindowsDrivePath returns true if the path looks like a Windows drive path (e.g., "C:/path")
func IsWindowsDrivePath(candidate string) bool {
	if len(candidate) < 3 {
		return false
	}
	return ((candidate[0] >= 'a' && candidate[0] <= 'z') || (candidate[0] >= 'A' && candidate[0] <= 'Z')) &&
		candidate[1] == ':' &&
		(candidate[2] == '/' || candidate[2] == '\\')
}

// GetCurrentContainerMounts inspects Arcane's own container and returns its bind and
// named-volume mounts as HostMount entries. It returns no mounts when Arcane is
// not running in a container (or the daemon is unreachable). This is the basis for
// Docker-in-Docker host-path resolution.
func GetCurrentContainerMounts(ctx context.Context, dockerCli *client.Client) ([]HostMount, error) {
	if dockerCli == nil {
		return nil, nil // No docker client, can't discover
	}

	// Label-fallback detection matters here: with network_mode: service:<sidecar>
	// the hostname identifies the sidecar, whose mounts do not include
	// docker.sock — breaking host-path resolution for the self-upgrader (#3544).
	inspect, err := libarcane.InspectCurrentArcaneContainer(ctx, dockerCli)
	if err != nil {
		// Not running in a container or can't reach docker daemon
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

// GetHostPathForContainerPath attempts to discover the host-side path for a given container path
// by inspecting the container itself. This is useful for Docker-in-Docker scenarios
// where the application needs to know host paths for volume mapping. It returns an empty
// string when the path is not covered by any of Arcane's mounts.
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
