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

	"github.com/getarcaneapp/arcane/backend/buildables"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	docker "github.com/getarcaneapp/arcane/backend/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	libupdater "github.com/getarcaneapp/arcane/backend/pkg/libarcane/imageupdate"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/cache"
	"github.com/getarcaneapp/arcane/types/version"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	ref "go.podman.io/image/v5/docker/reference"
	"golang.org/x/mod/semver"
)

const (
	versionTTL                     = 3 * time.Hour
	versionCheckURL                = "https://api.github.com/repos/getarcaneapp/arcane/releases/latest"
	arcaneManifestURL              = "https://raw.githubusercontent.com/getarcaneapp/arcane/main/.arcane.json"
	defaultRequestTimeout          = 15 * time.Second
	breakingChangesUnavailableText = "Automatic update is blocked because Arcane could not verify the remote breaking changes manifest. Try again later, or review the release notes and update Arcane manually."
)

type breakingChange struct {
	Version string `json:"version"`
	Message string `json:"message,omitempty"`
}

type arcaneManifest struct {
	Version         string           `json:"version,omitempty"`
	Revision        string           `json:"revision,omitempty"`
	BuildTime       string           `json:"buildTime,omitempty"`
	BreakingChanges []breakingChange `json:"breakingChanges,omitempty"`
}

type latestRelease struct {
	TagName     string
	Body        string
	PublishedAt string
}

type VersionService struct {
	httpClient               *http.Client
	cache                    *cache.Cache[latestRelease]
	breakingChangesCache     *cache.Cache[arcaneManifest]
	latestReleaseURL         string
	arcaneManifestURL        string
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
		cache:                    cache.New[latestRelease](versionTTL),
		breakingChangesCache:     cache.New[arcaneManifest](versionTTL),
		latestReleaseURL:         versionCheckURL,
		arcaneManifestURL:        arcaneManifestURL,
		disabled:                 disabled,
		version:                  version,
		revision:                 revision,
		containerRegistryService: containerRegistryService,
		dockerService:            dockerService,
	}
}

func (s *VersionService) getLatestReleaseInternal(ctx context.Context) (latestRelease, error) {
	rel, err := s.cache.GetOrFetch(ctx, func(ctx context.Context) (latestRelease, error) {
		reqCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, s.latestReleaseURL, nil)
		if err != nil {
			return latestRelease{}, fmt.Errorf("create GitHub request: %w", err)
		}

		resp, err := s.httpClient.Do(req) //nolint:gosec // intentional request to configured GitHub releases API endpoint
		if err != nil {
			return latestRelease{}, fmt.Errorf("get latest release: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return latestRelease{}, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		}

		var payload struct {
			TagName     string `json:"tag_name"`
			Body        string `json:"body"`
			PublishedAt string `json:"published_at"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return latestRelease{}, fmt.Errorf("decode payload: %w", err)
		}
		if payload.TagName == "" {
			return latestRelease{}, fmt.Errorf("GitHub API returned empty tag name")
		}

		return latestRelease{
			TagName:     payload.TagName,
			Body:        payload.Body,
			PublishedAt: payload.PublishedAt,
		}, nil
	})

	if staleErr, ok := errors.AsType[*cache.ErrStale](err); ok {
		slog.Warn("Failed to fetch latest release, returning stale cache", "error", staleErr.Err)
		return rel, nil
	}

	return rel, err
}

func (s *VersionService) getArcaneManifestInternal(ctx context.Context) (arcaneManifest, error) {
	manifest, err := s.breakingChangesCache.GetOrFetch(ctx, func(ctx context.Context) (arcaneManifest, error) {
		reqCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, s.arcaneManifestURL, nil)
		if err != nil {
			return arcaneManifest{}, fmt.Errorf("create arcane manifest request: %w", err)
		}

		resp, err := s.httpClient.Do(req) //nolint:gosec // production URL is fixed; tests inject a local manifest URL
		if err != nil {
			return arcaneManifest{}, fmt.Errorf("get arcane manifest: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return arcaneManifest{}, fmt.Errorf("arcane manifest returned status %d", resp.StatusCode)
		}

		var payload arcaneManifest
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return arcaneManifest{}, fmt.Errorf("decode arcane manifest: %w", err)
		}

		return payload, nil
	})

	if staleErr, ok := errors.AsType[*cache.ErrStale](err); ok {
		slog.Warn("Failed to fetch arcane manifest, returning stale cache", "error", staleErr.Err)
		return manifest, nil
	}

	return manifest, err
}

func (s *VersionService) GetLatestVersion(ctx context.Context) (string, error) {
	rel, err := s.getLatestReleaseInternal(ctx)
	return rel.TagName, err
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

// BreakingChangeRequirement reports whether the current-to-target app upgrade
// crosses a breaking change that cannot be handled by the self-updater.
func (s *VersionService) BreakingChangeRequirement(ctx context.Context, currentVersion string, targetVersion string) (bool, string) {
	if currentVersion == "" {
		currentVersion = s.version
	}

	current, currentOK := s.normalizeComparableVersionInternal(currentVersion)
	if !currentOK {
		return false, ""
	}

	manifest, err := s.getArcaneManifestInternal(ctx)
	if err != nil {
		slog.DebugContext(ctx, "Failed to determine breaking change requirement from manifest", "error", err)
		return true, breakingChangesUnavailableText
	}

	nextBreakingChange, hasFutureBreakingChange := s.nextBreakingChangeInternal(current, manifest.BreakingChanges)
	if !hasFutureBreakingChange {
		return false, ""
	}

	if targetVersion == "" && s.disabled {
		return true, breakingChangeMessageInternal(nextBreakingChange)
	}
	if targetVersion == "" {
		latest, err := s.GetLatestVersion(ctx)
		if err != nil {
			slog.DebugContext(ctx, "Failed to determine latest version for breaking change requirement", "error", err)
			return true, breakingChangeMessageInternal(nextBreakingChange)
		}
		targetVersion = latest
	}

	target, targetOK := s.normalizeComparableVersionInternal(targetVersion)
	if !targetOK {
		return false, ""
	}

	if change, crossed := s.crossedBreakingChangeInternal(current, target, manifest.BreakingChanges); crossed {
		return true, breakingChangeMessageInternal(change)
	}

	return false, ""
}

func (s *VersionService) nextBreakingChangeInternal(current string, changes []breakingChange) (breakingChange, bool) {
	var selected breakingChange
	var selectedVersion string
	found := false

	for _, change := range changes {
		changeVersion, ok := s.normalizeComparableVersionInternal(change.Version)
		if !ok || semver.Compare(current, changeVersion) >= 0 {
			continue
		}
		if !found || semver.Compare(changeVersion, selectedVersion) < 0 {
			selected = change
			selectedVersion = changeVersion
			found = true
		}
	}

	return selected, found
}

func (s *VersionService) crossedBreakingChangeInternal(current string, target string, changes []breakingChange) (breakingChange, bool) {
	var selected breakingChange
	var selectedVersion string
	found := false

	for _, change := range changes {
		changeVersion, ok := s.normalizeComparableVersionInternal(change.Version)
		if !ok {
			continue
		}
		if semver.Compare(current, changeVersion) < 0 && semver.Compare(target, changeVersion) >= 0 {
			if !found || semver.Compare(changeVersion, selectedVersion) < 0 {
				selected = change
				selectedVersion = changeVersion
				found = true
			}
		}
	}

	return selected, found
}

func breakingChangeMessageInternal(change breakingChange) string {
	message := strings.TrimSpace(change.Message)
	if message != "" {
		return message
	}

	return fmt.Sprintf("This update crosses the %s breaking change boundary and cannot be installed automatically. Review the release notes and update Arcane manually.", change.Version)
}

func (s *VersionService) normalizeComparableVersionInternal(ver string) (string, bool) {
	ver = strings.TrimSpace(ver)
	if ver == "" {
		return "", false
	}

	withoutPrefix := strings.TrimPrefix(ver, "v")
	if withoutPrefix == "" || withoutPrefix[0] < '0' || withoutPrefix[0] > '9' {
		return "", false
	}

	normalized := s.normalizeVersion(ver)
	return normalized, semver.IsValid(normalized)
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
		if staleErr, ok := errors.AsType[*cache.ErrStale](err); ok {
			slog.Warn("Failed to refresh latest version; using stale cache", "error", staleErr.Err)
		} else {
			return check, err
		}
	}

	if latest != "" {
		check.NewestVersion = latest
		check.UpdateAvailable = s.IsNewer(latest, cur)
		check.ReleaseURL = s.ReleaseURL(latest)
		if check.UpdateAvailable {
			check.BreakingChangeRequired, check.BreakingChangeMessage = s.BreakingChangeRequirement(ctx, cur, latest)
		}
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
		EnabledFeatures: parseEnabledFeatures(),
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
		rel, err := s.getLatestReleaseInternal(ctx)
		var staleErr *cache.ErrStale
		if err == nil || errors.As(err, &staleErr) {
			if rel.TagName != "" {
				info.NewestVersion = rel.TagName
				info.UpdateAvailable = s.IsNewer(rel.TagName, ver)
				info.ReleaseURL = s.ReleaseURL(rel.TagName)
				info.ReleaseNotes = rel.Body
				info.ReleasedAt = rel.PublishedAt
				if info.UpdateAvailable {
					info.BreakingChangeRequired, info.BreakingChangeMessage = s.BreakingChangeRequirement(ctx, ver, rel.TagName)
				}
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

	// Best-effort: pull release notes for non-semver track too, so the modal can preview
	// the latest tagged release even when the running build is digest-tracking.
	if !isSemver {
		if rel, err := s.getLatestReleaseInternal(ctx); err == nil && rel.TagName != "" {
			info.ReleaseNotes = rel.Body
			info.ReleasedAt = rel.PublishedAt
			if info.ReleaseURL == "" {
				info.ReleaseURL = s.ReleaseURL(rel.TagName)
			}
		}
	}

	return info
}

func parseEnabledFeatures() []string {
	raw := strings.TrimSpace(buildables.EnabledFeatures)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	features := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		feature := strings.ToLower(strings.TrimSpace(part))
		if feature == "" {
			continue
		}
		if _, exists := seen[feature]; exists {
			continue
		}
		seen[feature] = struct{}{}
		features = append(features, feature)
	}
	return features
}

// detectCurrentImageInfo attempts to detect the current container's image tag and digest
func (s *VersionService) detectCurrentImageInfo(ctx context.Context) (tag string, digest string, imageRef string) {
	if s.dockerService == nil {
		slog.Debug("detectCurrentImageInfo: dockerService is nil")
		return "", "", ""
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
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

	inspectResult, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerId, client.ContainerInspectOptions{})
	if err != nil {
		slog.Debug("detectCurrentImageInfo: failed to inspect container", "containerId", containerId, "error", err)
		return "", "", ""
	}
	container := inspectResult.Container

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
	f := make(client.Filters)
	f = f.Add("label", libupdater.LabelArcane+"=true")
	list, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		slog.Debug("findArcaneContainerByLabel: failed to list containers", "error", err)
		return ""
	}
	slog.Debug("findArcaneContainerByLabel: found containers with arcane label", "count", len(list.Items))

	var fallbackID string
	for _, c := range list.Items {
		slog.Debug("findArcaneContainerByLabel: checking container", "id", c.ID[:12], "state", c.State, "labels", c.Labels)
		// Skip the upgrader helper container
		if v, ok := c.Labels["com.getarcaneapp.arcane.upgrader"]; ok && strings.EqualFold(strings.TrimSpace(v), "true") {
			slog.Debug("findArcaneContainerByLabel: skipping upgrader container", "id", c.ID[:12])
			continue
		}
		// Prefer running containers
		if c.State == containertypes.StateRunning {
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
