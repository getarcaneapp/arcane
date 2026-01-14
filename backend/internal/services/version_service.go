package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/utils/arcaneupdater"
	"github.com/getarcaneapp/arcane/backend/internal/utils/cache"
	"github.com/getarcaneapp/arcane/backend/internal/utils/docker"
	"github.com/getarcaneapp/arcane/types/version"
	ref "go.podman.io/image/v5/docker/reference"
	"golang.org/x/mod/semver"
)

const (
	versionTTL            = 3 * time.Hour
	versionCheckURL       = "https://api.github.com/repos/getarcaneapp/arcane/releases/latest"
	defaultRequestTimeout = 5 * time.Second
)

type VersionService struct {
	httpClient               *http.Client
	cache                    *cache.Cache[string]
	disabled                 bool
	version                  string
	revision                 string
	containerRegistryService *ContainerRegistryService
	dockerService            *DockerClientService
}

func NewVersionService(httpClient *http.Client, disabled bool, version string, revision string, containerRegistryService *ContainerRegistryService, dockerService *DockerClientService) *VersionService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &VersionService{
		httpClient:               httpClient,
		cache:                    cache.New[string](versionTTL),
		disabled:                 disabled,
		version:                  version,
		revision:                 revision,
		containerRegistryService: containerRegistryService,
		dockerService:            dockerService,
	}
}

func (s *VersionService) GetLatestVersion(ctx context.Context) (string, error) {
	version, err := s.cache.GetOrFetch(ctx, func(ctx context.Context) (string, error) {
		reqCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, versionCheckURL, nil)
		if err != nil {
			return "", fmt.Errorf("create GitHub request: %w", err)
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("get latest release: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		}

		var payload struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return "", fmt.Errorf("decode payload: %w", err)
		}
		if payload.TagName == "" {
			return "", fmt.Errorf("GitHub API returned empty tag name")
		}

		return payload.TagName, nil
	})

	var staleErr *cache.ErrStale
	if errors.As(err, &staleErr) {
		slog.Warn("Failed to fetch latest version, returning stale cache", "error", staleErr.Err)
		return version, nil
	}

	return version, err
}

func (s *VersionService) IsNewer(latest, current string) bool {
	// Ensure both versions have 'v' prefix for semver package
	latest = s.normalizeVersion(latest)
	current = s.normalizeVersion(current)

	// Use semver.Compare: returns 1 if latest > current
	return semver.Compare(latest, current) > 0
}

// normalizeVersion ensures version has 'v' prefix and is valid semver format
func (s *VersionService) normalizeVersion(ver string) string {
	ver = strings.TrimSpace(ver)
	if ver == "" {
		return "v0.0.0"
	}
	if !strings.HasPrefix(ver, "v") {
		ver = "v" + ver
	}
	// If not valid semver, try to make it valid
	if !semver.IsValid(ver) {
		// Extract just the numeric part before any suffix
		if idx := strings.IndexAny(ver, "-+"); idx > 0 {
			ver = ver[:idx]
		}
		// Ensure at least v0.0.0 format
		parts := strings.Split(strings.TrimPrefix(ver, "v"), ".")
		for len(parts) < 3 {
			parts = append(parts, "0")
		}
		ver = "v" + strings.Join(parts[:3], ".")
	}
	return ver
}

func (s *VersionService) ReleaseURL(version string) string {
	if strings.TrimSpace(version) == "" {
		return "https://github.com/getarcaneapp/arcane/releases/latest"
	}

	v := strings.TrimSpace(version)
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return "https://github.com/getarcaneapp/arcane/releases/tag/" + v
}

func (s *VersionService) GetVersionInformation(ctx context.Context, currentVersion string) (*version.Check, error) {
	if currentVersion == "" {
		currentVersion = s.version
	}
	cur := s.normalizeVersion(currentVersion)

	check := &version.Check{
		CurrentVersion:  cur,
		ReleaseURL:      s.ReleaseURL(""),
		UpdateAvailable: false,
	}

	if s.disabled {
		return check, nil
	}

	latest, err := s.GetLatestVersion(ctx)
	if err != nil {
		var staleErr *cache.ErrStale
		if errors.As(err, &staleErr) {
			slog.Warn("Failed to refresh latest version; using stale cache", "error", staleErr.Err)
		} else {
			return check, err
		}
	}

	if latest != "" {
		check.NewestVersion = latest
		check.UpdateAvailable = s.IsNewer(latest, cur)
		check.ReleaseURL = s.ReleaseURL(latest)
	}

	return check, nil
}

// isSemverVersion checks if a version string is semver-based (e.g., v1.0.0)
func (s *VersionService) isSemverVersion() bool {
	version := strings.TrimSpace(s.version)
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return semver.IsValid(version)
}

// getDisplayVersion formats the version for display purposes
// If version contains "next", it returns "next-<short revision>"
// Otherwise returns the version as-is
func (s *VersionService) getDisplayVersion() string {
	version := strings.TrimPrefix(strings.TrimSpace(s.version), "v")
	if strings.Contains(strings.ToLower(version), "next") && s.revision != "" && s.revision != "unknown" {
		return fmt.Sprintf("next-%s", config.ShortRevision())
	}
	if s.isSemverVersion() {
		return "v" + version
	}
	return version
}

// GetAppVersionInfo returns application version information including display version
func (s *VersionService) GetAppVersionInfo(ctx context.Context) *version.Info {
	isSemver := s.isSemverVersion()
	ver := s.normalizeVersion(s.version)

	// Always detect current image info
	currentTag, currentDigest, currentImageRef := s.detectCurrentImageInfo(ctx)

	// Build base info struct (always populated)
	info := &version.Info{
		CurrentVersion:  ver,
		CurrentTag:      currentTag,
		CurrentDigest:   currentDigest,
		DisplayVersion:  s.getDisplayVersion(),
		Revision:        s.revision,
		ShortRevision:   config.ShortRevision(),
		GoVersion:       config.GoVersion(),
		BuildTime:       config.BuildTime,
		IsSemverVersion: isSemver,
		UpdateAvailable: false,
	}

	// If update checks disabled, return base info
	if s.disabled {
		return info
	}

	// For semver versions, check GitHub releases
	if isSemver {
		latest, err := s.GetLatestVersion(ctx)
		var staleErr *cache.ErrStale
		if err == nil || errors.As(err, &staleErr) {
			if latest != "" {
				info.NewestVersion = latest
				info.UpdateAvailable = s.IsNewer(latest, ver)
				info.ReleaseURL = s.ReleaseURL(latest)
			}
		}
		return info
	}

	// For non-semver versions (like "next"), check digest-based updates
	if currentTag != "" && currentDigest != "" && currentImageRef != "" && s.containerRegistryService != nil {
		updateAvailable, latestDigest := s.checkDigestBasedUpdate(ctx, currentTag, currentDigest, currentImageRef)
		info.UpdateAvailable = updateAvailable
		info.NewestDigest = latestDigest
	}

	return info
}

// detectCurrentImageInfo attempts to detect the current container's image tag and digest
func (s *VersionService) detectCurrentImageInfo(ctx context.Context) (tag string, digest string, imageRef string) {
	if s.dockerService == nil {
		slog.Debug("detectCurrentImageInfo: dockerService is nil")
		return "", "", ""
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		slog.Debug("detectCurrentImageInfo: failed to get docker client", "error", err)
		return "", "", ""
	}

	containerId := s.detectContainerID(ctx, dockerClient)
	if containerId == "" {
		slog.Debug("detectCurrentImageInfo: could not detect container ID")
		return "", "", ""
	}
	slog.Debug("detectCurrentImageInfo: detected container", "containerId", containerId)

	container, err := dockerClient.ContainerInspect(ctx, containerId)
	if err != nil {
		slog.Debug("detectCurrentImageInfo: failed to inspect container", "containerId", containerId, "error", err)
		return "", "", ""
	}

	// Parse tag from container config image (user-specified reference)
	tag = s.extractTagFromImageRef(container.Config.Image)

	// Get digest and normalized imageRef from container image
	imageRef, digest = s.extractImageDetails(ctx, dockerClient, container)

	// Fallback to container config image if RepoDigests didn't provide imageRef
	if imageRef == "" {
		imageRef = s.normalizeImageRef(container.Config.Image)
	}

	return tag, digest, imageRef
}

// detectContainerID tries to get the current container ID, falling back to label-based detection
func (s *VersionService) detectContainerID(ctx context.Context, dockerClient *client.Client) string {
	containerId, err := s.getCurrentContainerID()
	if err == nil {
		slog.Debug("detectContainerID: found via getCurrentContainerID", "containerId", containerId)
		return containerId
	}
	slog.Debug("detectContainerID: getCurrentContainerID failed, trying label fallback", "error", err)

	// Fallback: locate the Arcane container by label (works even when cgroup/hostname detection fails)
	return s.findArcaneContainerByLabel(ctx, dockerClient)
}

// findArcaneContainerByLabel searches for the Arcane container using labels
func (s *VersionService) findArcaneContainerByLabel(ctx context.Context, dockerClient *client.Client) string {
	f := filters.NewArgs()
	f.Add("label", arcaneupdater.LabelArcane+"=true")
	list, err := dockerClient.ContainerList(ctx, containertypes.ListOptions{All: true, Filters: f})
	if err != nil {
		slog.Debug("findArcaneContainerByLabel: failed to list containers", "error", err)
		return ""
	}
	slog.Debug("findArcaneContainerByLabel: found containers with arcane label", "count", len(list))

	var fallbackID string
	for _, c := range list {
		slog.Debug("findArcaneContainerByLabel: checking container", "id", c.ID[:12], "state", c.State, "labels", c.Labels)
		// Skip the upgrader helper container
		if v, ok := c.Labels["com.getarcaneapp.arcane.upgrader"]; ok && strings.EqualFold(strings.TrimSpace(v), "true") {
			slog.Debug("findArcaneContainerByLabel: skipping upgrader container", "id", c.ID[:12])
			continue
		}
		// Prefer running containers
		if strings.EqualFold(strings.TrimSpace(c.State), "running") {
			slog.Debug("findArcaneContainerByLabel: found running container", "id", c.ID[:12])
			return c.ID
		}
		if fallbackID == "" {
			fallbackID = c.ID
		}
	}
	if fallbackID != "" {
		slog.Debug("findArcaneContainerByLabel: using fallback container", "id", fallbackID[:12])
	} else {
		slog.Debug("findArcaneContainerByLabel: no container found")
	}
	return fallbackID
}

// extractImageDetails extracts digest and imageRef from a container's image
func (s *VersionService) extractImageDetails(ctx context.Context, dockerClient *client.Client, container containertypes.InspectResponse) (imageRef, digest string) {
	if container.Image == "" {
		return "", ""
	}

	imageInspect, err := dockerClient.ImageInspect(ctx, container.Image)
	if err != nil {
		return "", ""
	}

	// Extract digest and repository from first RepoDigest using reference library
	for _, repoDigest := range imageInspect.RepoDigests {
		named, err := ref.ParseNormalizedNamed(repoDigest)
		if err != nil {
			continue
		}
		if digested, ok := named.(ref.Digested); ok {
			return named.Name(), string(digested.Digest())
		}
	}

	return "", ""
}

// normalizeImageRef extracts just the repository name from an image reference
func (s *VersionService) normalizeImageRef(configImage string) string {
	if named, err := ref.ParseNormalizedNamed(configImage); err == nil {
		return named.Name()
	}
	return configImage
}

// getCurrentContainerID detects if we're running in Docker via cgroup, mountinfo, or hostname
func (s *VersionService) getCurrentContainerID() (string, error) {
	return docker.GetCurrentContainerID()
}

// extractTagFromImageRef extracts the tag from an image reference using distribution/reference
func (s *VersionService) extractTagFromImageRef(imageRef string) string {
	named, err := ref.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "latest"
	}

	tagged, ok := named.(ref.Tagged)
	if ok {
		return tagged.Tag()
	}

	return "latest"
}

// checkDigestBasedUpdate checks if there's a newer digest for the current tag
func (s *VersionService) checkDigestBasedUpdate(ctx context.Context, currentTag, currentDigest, currentImageRef string) (updateAvailable bool, latestDigest string) {
	if currentTag == "" || currentDigest == "" || currentImageRef == "" {
		return false, ""
	}

	// Build full image reference with tag
	imageRef := fmt.Sprintf("%s:%s", currentImageRef, currentTag)

	// Fetch latest digest from registry
	latestDigest, err := s.containerRegistryService.GetImageDigest(ctx, imageRef)
	if err != nil {
		slog.WarnContext(ctx, "Failed to fetch latest digest for tag", "tag", currentTag, "error", err)
		return false, ""
	}

	// Compare digests - if they differ, an update is available
	updateAvailable = currentDigest != latestDigest && latestDigest != ""

	if updateAvailable {
		slog.InfoContext(ctx, "Digest-based update available", "tag", currentTag, "currentDigest", currentDigest, "latestDigest", latestDigest)
	}

	return updateAvailable, latestDigest
}
