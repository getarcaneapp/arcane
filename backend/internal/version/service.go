package version

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"emperror.dev/errors"

	ref "github.com/distribution/reference"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"golang.org/x/mod/semver"

	"github.com/getarcaneapp/arcane/backend/v2/buildables"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apns"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/version"
	"github.com/samber/hot"
	"go.getarcane.app/sys/cgroup"
)

const (
	versionTTL            = 3 * time.Hour
	versionCheckURL       = "https://api.github.com/repos/getarcaneapp/arcane/releases/latest"
	defaultRequestTimeout = 15 * time.Second
)

type latestRelease struct {
	TagName     string
	Body        string
	PublishedAt string
}

type VersionService struct {
	httpClient               *http.Client
	cache                    *hot.HotCache[struct{}, latestRelease]
	disabled                 bool
	version                  string
	revision                 string
	containerRegistryService *registry.ContainerRegistryService
	dockerService            *docker.DockerClientService
	imageUpdateService       *imageupdate.ImageUpdateService
	settingsService          *settings.SettingsService
}

func NewVersionService(httpClient *http.Client, disabled bool, version string, revision string, containerRegistryService *registry.ContainerRegistryService, dockerService *docker.DockerClientService, imageUpdateService *imageupdate.ImageUpdateService, settingsService *settings.SettingsService) *VersionService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	service := &VersionService{
		httpClient:               httpClient,
		disabled:                 disabled,
		version:                  version,
		revision:                 revision,
		containerRegistryService: containerRegistryService,
		dockerService:            dockerService,
		imageUpdateService:       imageUpdateService,
		settingsService:          settingsService,
	}
	loader := func(_ []struct{}) (map[struct{}]latestRelease, error) {
		ctx, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
		defer cancel()
		release, err := service.fetchLatestReleaseInternal(ctx)
		if err != nil {
			return nil, err
		}
		return map[struct{}]latestRelease{{}: release}, nil
	}
	service.cache = hot.NewHotCache[struct{}, latestRelease](hot.LRU, 1).
		WithTTL(versionTTL).
		WithLoaders(loader).
		WithRevalidation(24*time.Hour, loader).
		WithRevalidationErrorPolicy(hot.KeepOnError).
		Build()
	return service
}

func (s *VersionService) getLatestReleaseInternal(_ context.Context) (latestRelease, error) {
	release, found, err := s.cache.Get(struct{}{})
	if err != nil {
		return latestRelease{}, err
	}
	if !found {
		return latestRelease{}, errors.New("latest release cache loader returned no release")
	}
	return release, nil
}

func (s *VersionService) fetchLatestReleaseInternal(ctx context.Context) (latestRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, versionCheckURL, nil)
	if err != nil {
		return latestRelease{}, errors.WrapIf(err, "create GitHub request")
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return latestRelease{}, errors.WrapIf(err, "get latest release")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return latestRelease{}, errors.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var payload struct {
		TagName     string `json:"tag_name"`
		Body        string `json:"body"`
		PublishedAt string `json:"published_at"`
	}
	if err := json.UnmarshalRead(resp.Body, &payload); err != nil {
		return latestRelease{}, errors.WrapIf(err, "decode payload")
	}
	if payload.TagName == "" {
		return latestRelease{}, errors.New("GitHub API returned empty tag name")
	}

	return latestRelease{
		TagName:     payload.TagName,
		Body:        payload.Body,
		PublishedAt: payload.PublishedAt,
	}, nil
}

func (s *VersionService) GetLatestVersion(ctx context.Context) (string, error) {
	rel, err := s.getLatestReleaseInternal(ctx)
	if err != nil {
		return "", err
	}
	return rel.TagName, nil
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
	trimmed := strings.TrimPrefix(ver, "v")
	if trimmed == "" || trimmed[0] < '0' || trimmed[0] > '9' {
		return ver
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
		return check, err
	}

	// Skip when latest is semver-older than current (e.g. a prerelease build
	// asking the stable release feed) — a downgrade is not a newest version.
	if latest != "" && !s.IsNewer(cur, latest) {
		check.NewestVersion = latest
		check.UpdateAvailable = s.IsNewer(latest, cur)
		check.ReleaseURL = s.ReleaseURL(latest)
	}

	return check, nil
}

// isNextBuildInternal reports whether this build tracks the next channel: a
// -next. prerelease version, or a container running the rolling next image tag.
func (s *VersionService) isNextBuildInternal(currentTag string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(s.version)), "-next.") ||
		strings.TrimSpace(currentTag) == "next"
}

// resolveNextVersionInternal returns the normalized version label of the image
// an upgrade would pull, or "" when it cannot be resolved. It prefers the
// digest reference so the reported version and NewestDigest describe the same
// artifact.
func (s *VersionService) resolveNextVersionInternal(ctx context.Context, imageRef, tag, newestDigest string) string {
	if s.containerRegistryService == nil || strings.TrimSpace(imageRef) == "" {
		return ""
	}

	var lookupRef string
	switch {
	case strings.TrimSpace(newestDigest) != "":
		lookupRef = imageRef + "@" + strings.TrimSpace(newestDigest)
	case strings.TrimSpace(tag) != "":
		lookupRef = imageRef + ":" + strings.TrimSpace(tag)
	default:
		return ""
	}

	label, err := s.containerRegistryService.ImageVersionLabel(ctx, lookupRef)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve next-channel version label", "imageRef", lookupRef, "error", err)
		return ""
	}

	normalized := s.normalizeVersion(label)
	if !semver.IsValid(normalized) {
		slog.WarnContext(ctx, "Next-channel image version label is not valid semver", "imageRef", lookupRef, "label", label)
		return ""
	}
	return normalized
}

// isSemverVersion checks if a version string is semver-based (e.g., v1.0.0)
func (s *VersionService) isSemverVersion() bool {
	versionValue := strings.TrimSpace(s.version)
	if !strings.HasPrefix(versionValue, "v") {
		versionValue = "v" + versionValue
	}
	return semver.IsValid(versionValue)
}

// getDisplayVersion formats the version for display purposes
// Semver versions (including prereleases like 2.4.0-next.1) display as v<version>
func (s *VersionService) getDisplayVersion() string {
	versionValue := strings.TrimPrefix(strings.TrimSpace(s.version), "v")
	if s.isSemverVersion() {
		return "v" + versionValue
	}
	return versionValue
}

// updateCheckImageRefInternal points the digest lookup at the configured registry; "auto" keeps the running reference.
func (s *VersionService) updateCheckImageRefInternal(currentImageRef string) string {
	if s.settingsService == nil {
		return currentImageRef
	}
	target := strings.TrimSpace(s.settingsService.GetSettingsConfig().UpdateCheckRegistry.Value)
	if target == "" || target == "auto" {
		return currentImageRef
	}

	named, err := ref.ParseNormalizedNamed(strings.TrimSpace(currentImageRef))
	if err != nil || !strings.HasPrefix(ref.Path(named), "getarcaneapp/") {
		return currentImageRef
	}

	host := libarcane.ArcaneRegistryHost(target)
	repoPath := ref.Path(named)
	if host == libarcane.DockerHubRegistryHost {
		switch repoPath {
		case "getarcaneapp/arcane":
			repoPath = "getarcaneapp/manager"
		case "getarcaneapp/arcane-headless":
			repoPath = "getarcaneapp/agent"
		}
	}
	if host == ref.Domain(named) && repoPath == ref.Path(named) {
		return currentImageRef
	}
	return host + "/" + repoPath
}

// GetAppVersionInfo returns application version information including display version
func (s *VersionService) GetAppVersionInfo(ctx context.Context) *version.Info {
	isSemver := s.isSemverVersion()
	ver := s.normalizeVersion(s.version)

	// Always detect current image info
	currentTag, currentDigest, currentImageRef, currentImageID := s.detectCurrentImageInfo(ctx)

	// Build base info struct (always populated)
	info := &version.Info{
		CurrentVersion:   ver,
		CurrentTag:       currentTag,
		CurrentDigest:    currentDigest,
		DisplayVersion:   s.getDisplayVersion(),
		Revision:         s.revision,
		ShortRevision:    config.ShortRevision(),
		GoVersion:        config.GoVersion(),
		NodeVersion:      config.NodeVersion,
		SvelteKitVersion: config.SvelteKitVersion,
		EnabledFeatures:  append(utils.UniqueNonEmptyStrings(strings.Split(strings.ToLower(buildables.EnabledFeatures), ",")), apns.FeatureName),
		BuildTime:        config.BuildTime,
		IsSemverVersion:  isSemver,
		UpdateAvailable:  false,
	}

	// If update checks disabled, return base info
	if s.disabled {
		return info
	}

	checkImageRef := s.updateCheckImageRefInternal(currentImageRef)
	digestUpdateAvailable, latestDigest := s.storedOrDigestBasedUpdateInternal(ctx, currentImageID, currentTag, currentDigest, currentImageRef, checkImageRef)
	if latestDigest != "" {
		info.NewestDigest = latestDigest
	}

	switch {
	case isSemver && s.isNextBuildInternal(currentTag):
		nextVersion := s.resolveNextVersionInternal(ctx, checkImageRef, currentTag, latestDigest)
		if nextVersion != "" && semver.Compare(nextVersion, ver) >= 0 {
			info.NewestVersion = nextVersion
		}
		info.UpdateAvailable = digestUpdateAvailable || (nextVersion != "" && s.IsNewer(nextVersion, ver))
	case isSemver:
		// Never surface a release older than the running version as the target.
		rel, err := s.getLatestReleaseInternal(ctx)
		if err == nil && rel.TagName != "" && !s.IsNewer(ver, rel.TagName) {
			info.NewestVersion = rel.TagName
			info.UpdateAvailable = s.IsNewer(rel.TagName, ver)
			info.ReleaseURL = s.ReleaseURL(rel.TagName)
			info.ReleaseNotes = rel.Body
			info.ReleasedAt = rel.PublishedAt
		}
	default:
		// Best-effort: pull release notes for non-semver track too, so the modal can preview
		// the latest tagged release even when the running build is digest-tracking.
		if rel, err := s.getLatestReleaseInternal(ctx); err == nil && rel.TagName != "" {
			info.ReleaseNotes = rel.Body
			info.ReleasedAt = rel.PublishedAt
			if info.ReleaseURL == "" {
				info.ReleaseURL = s.ReleaseURL(rel.TagName)
			}
		}
		info.UpdateAvailable = digestUpdateAvailable
	}

	return info
}

// storedOrDigestBasedUpdateInternal uses the stored poller row only when the check targets the running reference.
func (s *VersionService) storedOrDigestBasedUpdateInternal(ctx context.Context, currentImageID, currentTag, currentDigest, currentImageRef, checkImageRef string) (bool, string) {
	if s.imageUpdateService != nil && strings.TrimSpace(currentImageID) != "" && checkImageRef == currentImageRef {
		record, found, err := s.imageUpdateService.StoredUpdateByImageID(ctx, currentImageID)
		if err != nil {
			slog.WarnContext(ctx, "Failed to read stored Arcane image update state", "imageID", currentImageID, "error", err)
		} else if found {
			return record.HasUpdate, mo.PointerToOption(record.LatestDigest).OrEmpty()
		}
	}

	if currentTag != "" && currentDigest != "" && checkImageRef != "" && s.containerRegistryService != nil {
		return s.checkDigestBasedUpdate(ctx, currentTag, currentDigest, checkImageRef)
	}

	return false, ""
}

// detectCurrentImageInfo attempts to detect the current container's image tag and digest
func (s *VersionService) detectCurrentImageInfo(ctx context.Context) (tag string, digest string, imageRef string, imageID string) {
	if s.dockerService == nil {
		slog.Debug("detectCurrentImageInfo: dockerService is nil")
		return "", "", "", ""
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		slog.Debug("detectCurrentImageInfo: failed to get docker client", "error", err)
		return "", "", "", ""
	}

	containerId := s.detectContainerID(ctx, dockerClient)
	if containerId == "" {
		slog.Debug("detectCurrentImageInfo: could not detect container ID")
		return "", "", "", ""
	}
	slog.Debug("detectCurrentImageInfo: detected container", "containerId", containerId)

	inspectResult, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerId, client.ContainerInspectOptions{})
	if err != nil {
		slog.Debug("detectCurrentImageInfo: failed to inspect container", "containerId", containerId, "error", err)
		return "", "", "", ""
	}
	container := inspectResult.Container
	imageID = container.Image

	configImage := ""
	if container.Config != nil {
		configImage = container.Config.Image
	}

	// Parse tag from container config image (user-specified reference)
	tag = s.extractTagFromImageRef(configImage)

	// Get digest and normalized imageRef from container image
	imageRef, digest = s.extractImageDetails(ctx, dockerClient, container)

	// Fallback to container config image if RepoDigests didn't provide imageRef
	if imageRef == "" {
		imageRef = s.normalizeImageRef(configImage)
	}

	return tag, digest, imageRef, imageID
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
	return libarcane.FindArcaneContainerIDByLabel(ctx, dockerClient)
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
	return cgroup.CurrentContainerID()
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
	latestDigest, err := s.containerRegistryService.ImageDigest(ctx, imageRef)
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
