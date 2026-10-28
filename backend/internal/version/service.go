package version

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/types/v2/version"
	"github.com/moby/moby/client"
	"github.com/samber/hot"
	"github.com/samber/mo"
	"go.getarcane.app/docker/compat"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/streams/agg"
	"go.getarcane.app/sys/cgroup"
	"golang.org/x/mod/semver"

	"github.com/getarcaneapp/arcane/backend/v2/buildables"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apns"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
)

const (
	versionStreamPollInterval = 5 * time.Minute
	versionTTL                = 3 * time.Hour
	versionCheckURL           = "https://api.github.com/repos/getarcaneapp/arcane/releases/latest"
	defaultRequestTimeout     = 15 * time.Second
)

type latestRelease struct {
	TagName     string
	Body        string
	PublishedAt string
}

type currentImageInfo struct {
	Tag          string
	Digest       string
	ImageRef     string
	ImageID      string
	DigestPinned bool
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
	streamHub                *agg.Hub[version.StreamEvent]
	currentImage             *hot.HotCache[struct{}, currentImageInfo]
}

func NewVersionService(
	httpClient *http.Client,
	disabled bool,
	appVersion, revision string,
	containerRegistryService *registry.ContainerRegistryService,
	dockerService *docker.DockerClientService,
	imageUpdateService *imageupdate.ImageUpdateService,
	settingsService *settings.SettingsService,
) *VersionService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	service := &VersionService{
		httpClient:               httpClient,
		disabled:                 disabled,
		version:                  appVersion,
		revision:                 revision,
		containerRegistryService: containerRegistryService,
		dockerService:            dockerService,
		imageUpdateService:       imageUpdateService,
		settingsService:          settingsService,
		streamHub:                agg.NewHub[version.StreamEvent](),
		currentImage:             hot.NewHotCache[struct{}, currentImageInfo](hot.LRU, 1).Build(),
	}
	loader := func(cacheFailure bool) func(_ []struct{}) (map[struct{}]latestRelease, error) {
		return func(_ []struct{}) (map[struct{}]latestRelease, error) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout) //nolint:forbidigo // Cache revalidation runs independently of request cancellation.
			defer cancel()
			release, err := service.fetchLatestRelease(ctx)
			switch {
			case err != nil && cacheFailure:
				slog.WarnContext(ctx, "Failed to fetch latest Arcane release; skipping update checks until the cache expires", "error", err)
				return map[struct{}]latestRelease{{}: {}}, nil
			case err != nil:
				return nil, err
			default:
				return map[struct{}]latestRelease{{}: release}, nil
			}
		}
	}
	service.cache = hot.NewHotCache[struct{}, latestRelease](hot.LRU, 1).
		WithTTL(versionTTL).
		WithLoaders(loader(true)).
		WithRevalidation(24*time.Hour, loader(false)).
		WithRevalidationErrorPolicy(hot.KeepOnError).
		Build()
	return service
}

func (s *VersionService) getLatestRelease(_ context.Context) (latestRelease, error) {
	release, found, err := s.cache.Get(struct{}{})
	if err != nil {
		return latestRelease{}, err
	}
	if !found {
		return latestRelease{}, errors.New("latest release cache loader returned no release")
	}
	return release, nil
}

func (s *VersionService) fetchLatestRelease(ctx context.Context) (latestRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, versionCheckURL, http.NoBody)
	if err != nil {
		return latestRelease{}, fmt.Errorf("create GitHub request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
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
	if unmarshalReadErr := json.UnmarshalRead(resp.Body, &payload); unmarshalReadErr != nil {
		return latestRelease{}, fmt.Errorf("decode payload: %w", unmarshalReadErr)
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
	rel, err := s.getLatestRelease(ctx)
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
	ver = kit.EnsurePrefix(ver, "v")
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

func (s *VersionService) ReleaseURL(localVersion string) string {
	if strings.TrimSpace(localVersion) == "" {
		return "https://github.com/getarcaneapp/arcane/releases/latest"
	}

	return "https://github.com/getarcaneapp/arcane/releases/tag/" + kit.EnsurePrefix(localVersion, "v")
}

func (s *VersionService) GetVersionInformation(ctx context.Context, currentVersion string) (*version.Check, error) {
	if currentVersion == "" {
		currentVersion = s.version
	}
	cur := s.normalizeVersion(currentVersion)

	latest, err := func() (string, error) {
		if s.disabled {
			return "", nil
		}
		return s.GetLatestVersion(ctx)
	}()
	// Skip when latest is semver-older than current (e.g. a prerelease build
	// asking the stable release feed) — a downgrade is not a newest version.
	newest := kit.Ternary(err == nil && latest != "" && !s.IsNewer(cur, latest), latest, "")
	return &version.Check{
		CurrentVersion:  cur,
		NewestVersion:   newest,
		UpdateAvailable: newest != "" && s.IsNewer(newest, cur),
		ReleaseURL:      s.ReleaseURL(newest),
	}, err
}

// isNextBuild reports whether this build tracks the next channel: a
// -next. prerelease version, or a container running the rolling next image tag.
func (s *VersionService) isNextBuild(currentTag string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(s.version)), "-next.") ||
		strings.TrimSpace(currentTag) == "next"
}

// resolveNextVersion returns the normalized version label of the image
// an upgrade would pull, or "" when it cannot be resolved. It prefers the
// digest reference so the reported version and NewestDigest describe the same
// artifact.
func (s *VersionService) resolveNextVersion(ctx context.Context, imageRef, tag, newestDigest string) string {
	newestDigest, tag = strings.TrimSpace(newestDigest), strings.TrimSpace(tag)
	if s.containerRegistryService == nil || strings.TrimSpace(imageRef) == "" || (newestDigest == "" && tag == "") {
		return ""
	}

	lookupRef := kit.Ternary(newestDigest != "", imageRef+"@"+newestDigest, imageRef+":"+tag)

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

// updateCheckImageRef points the digest lookup at the configured registry; "auto" keeps the running reference.
func (s *VersionService) updateCheckImageRef(currentImageRef string) string {
	if s.settingsService == nil {
		return currentImageRef
	}
	target := strings.TrimSpace(s.settingsService.GetSettingsConfig().UpdateCheckRegistry.Value)
	if target == "" || target == "auto" {
		return currentImageRef
	}

	named, err := reference.ParseNormalizedNamed(strings.TrimSpace(currentImageRef))
	if err != nil || !strings.HasPrefix(reference.Path(named), "getarcaneapp/") {
		return currentImageRef
	}

	host := libarcane.ArcaneImageRegistryHost(target)
	repoPath := reference.Path(named)
	// Docker Hub and ECR Public publish the images under the manager and agent names only
	if host == libarcane.DockerHubRegistryHost || host == libarcane.ECRPublicRegistryHost {
		switch repoPath {
		case "getarcaneapp/arcane":
			repoPath = "getarcaneapp/manager"
		case "getarcaneapp/arcane-headless":
			repoPath = "getarcaneapp/agent"
		}
	}
	return kit.Ternary(host == reference.Domain(named) && repoPath == reference.Path(named), currentImageRef, host+"/"+repoPath)
}

// GetAppVersionInfo returns application version information including display version
func (s *VersionService) GetAppVersionInfo(ctx context.Context) *version.Info {
	isSemver := semver.IsValid(kit.EnsurePrefix(s.version, "v"))
	ver := s.normalizeVersion(s.version)
	// Semver versions (including prereleases like 2.4.0-next.1) display as v<version>.
	displayVersion := strings.TrimPrefix(strings.TrimSpace(s.version), "v")

	current := s.detectCurrentImageInfo(ctx)

	// Update fields stay empty when update checks are disabled.
	update := func() version.Info {
		if s.disabled {
			return version.Info{}
		}
		checkImageRef := s.updateCheckImageRef(current.ImageRef)
		digestUpdateAvailable, latestDigest := s.storedOrDigestBasedUpdate(ctx, current, checkImageRef)

		switch {
		case isSemver && s.isNextBuild(current.Tag):
			nextVersion := s.resolveNextVersion(ctx, checkImageRef, current.Tag, latestDigest)
			return version.Info{
				NewestDigest:    latestDigest,
				NewestVersion:   kit.Ternary(nextVersion != "" && semver.Compare(nextVersion, ver) >= 0, nextVersion, ""),
				UpdateAvailable: digestUpdateAvailable || (nextVersion != "" && s.IsNewer(nextVersion, ver)),
			}
		case isSemver:
			// Never surface a release older than the running version as the target.
			rel, err := s.getLatestRelease(ctx)
			if err != nil || rel.TagName == "" || s.IsNewer(ver, rel.TagName) {
				return version.Info{NewestDigest: latestDigest}
			}
			return version.Info{
				NewestDigest:    latestDigest,
				NewestVersion:   rel.TagName,
				UpdateAvailable: s.IsNewer(rel.TagName, ver),
				ReleaseURL:      s.ReleaseURL(rel.TagName),
				ReleaseNotes:    rel.Body,
				ReleasedAt:      rel.PublishedAt,
			}
		}

		// Best-effort: pull release notes for non-semver track too, so the modal can preview
		// the latest tagged release even when the running build is digest-tracking.
		rel, err := s.getLatestRelease(ctx)
		if err != nil || rel.TagName == "" {
			return version.Info{NewestDigest: latestDigest, UpdateAvailable: digestUpdateAvailable}
		}
		return version.Info{
			NewestDigest:    latestDigest,
			UpdateAvailable: digestUpdateAvailable,
			ReleaseURL:      s.ReleaseURL(rel.TagName),
			ReleaseNotes:    rel.Body,
			ReleasedAt:      rel.PublishedAt,
		}
	}()

	return &version.Info{
		CurrentVersion:   ver,
		CurrentTag:       current.Tag,
		CurrentDigest:    current.Digest,
		DisplayVersion:   kit.Ternary(isSemver, "v"+displayVersion, displayVersion),
		Revision:         s.revision,
		ShortRevision:    config.ShortRevision(),
		GoVersion:        config.GoVersion(),
		NodeVersion:      config.NodeVersion,
		SvelteKitVersion: config.SvelteKitVersion,
		EnabledFeatures:  append(kit.Unique(kit.TrimNonEmpty(strings.Split(strings.ToLower(buildables.EnabledFeatures), ","))), apns.FeatureName),
		BuildTime:        config.BuildTime,
		IsSemverVersion:  isSemver,
		NewestVersion:    update.NewestVersion,
		NewestDigest:     update.NewestDigest,
		UpdateAvailable:  update.UpdateAvailable,
		ReleaseURL:       update.ReleaseURL,
		ReleaseNotes:     update.ReleaseNotes,
		ReleasedAt:       update.ReleasedAt,
	}
}

// storedOrDigestBasedUpdate uses the stored poller row only when the check targets the running, unpinned reference.
func (s *VersionService) storedOrDigestBasedUpdate(ctx context.Context, current currentImageInfo, checkImageRef string) (bool, string) {
	if s.imageUpdateService != nil && strings.TrimSpace(current.ImageID) != "" && checkImageRef == current.ImageRef && !current.DigestPinned {
		record, found, err := s.imageUpdateService.StoredUpdateByImageID(ctx, current.ImageID)
		if err != nil {
			slog.WarnContext(ctx, "Failed to read stored Arcane image update state", "imageId", current.ImageID, "error", err)
		} else if found {
			return record.HasUpdate, mo.PointerToOption(record.LatestDigest).OrEmpty()
		}
	}

	if current.Tag == "" || current.Digest == "" || checkImageRef == "" || s.containerRegistryService == nil {
		return false, ""
	}

	latestDigest, err := s.containerRegistryService.ImageDigest(ctx, checkImageRef+":"+current.Tag)
	if err != nil {
		slog.WarnContext(ctx, "Failed to fetch latest digest for tag", "tag", current.Tag, "error", err)
		return false, ""
	}

	updateAvailable := current.Digest != latestDigest && latestDigest != ""
	if updateAvailable {
		slog.InfoContext(ctx, "Digest-based update available", "tag", current.Tag, "currentDigest", current.Digest, "latestDigest", latestDigest)
	}
	return updateAvailable, latestDigest
}

// detectCurrentImageInfo attempts to detect the current container's image tag and digest
func (s *VersionService) detectCurrentImageInfo(ctx context.Context) (detected currentImageInfo) {
	if cached, found, _ := s.currentImage.Get(struct{}{}); found {
		return cached
	}
	defer func() {
		if ctx.Err() != nil {
			return
		}
		if detected.Digest != "" {
			s.currentImage.Set(struct{}{}, detected)
		} else {
			s.currentImage.SetWithTTL(struct{}{}, detected, time.Minute)
		}
	}()

	if s.dockerService == nil {
		slog.DebugContext(ctx, "detectCurrentImageInfo: dockerService is nil")
		return currentImageInfo{}
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		slog.DebugContext(ctx, "detectCurrentImageInfo: failed to get docker client", "error", err)
		return currentImageInfo{}
	}

	containerId, err := cgroup.CurrentContainerID()
	if err != nil {
		// Fall back to the Arcane label when cgroup/hostname detection fails.
		slog.DebugContext(ctx, "detectCurrentImageInfo: cgroup detection failed, trying label fallback", "error", err)
		containerId = libarcane.FindArcaneContainerIDByLabel(ctx, dockerClient)
	}
	if containerId == "" {
		slog.DebugContext(ctx, "detectCurrentImageInfo: could not detect container ID")
		return currentImageInfo{}
	}
	slog.DebugContext(ctx, "detectCurrentImageInfo: detected container", "containerId", containerId)

	inspectResult, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerInspectResult, error) {
		return compat.ContainerInspectWithCompatibility(ctx, cli, containerId, client.ContainerInspectOptions{})
	})
	if err != nil {
		slog.DebugContext(ctx, "detectCurrentImageInfo: failed to inspect container", "containerId", containerId, "error", err)
		return currentImageInfo{}
	}
	configImage := kit.FromPtr(inspectResult.Container.Config).Image

	// The tag comes from the user-specified config image; the repository falls back to it too.
	tag, imageRef := "latest", configImage
	named, parseErr := reference.ParseNormalizedNamed(configImage)
	if parseErr == nil {
		imageRef = named.Name()
		if tagged, ok := named.(reference.Tagged); ok {
			tag = tagged.Tag()
		}
	}
	_, digestPinned := named.(reference.Digested)
	base := currentImageInfo{
		Tag:          tag,
		ImageRef:     imageRef,
		ImageID:      inspectResult.Container.Image,
		DigestPinned: digestPinned,
	}

	if base.ImageID == "" {
		return base
	}
	imageInspect, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ImageInspectResult, error) {
		return cli.ImageInspect(ctx, base.ImageID)
	})
	if err != nil {
		return base
	}
	for _, repoDigest := range imageInspect.RepoDigests {
		repoNamed, repoParseErr := reference.ParseNormalizedNamed(repoDigest)
		if repoParseErr != nil {
			continue
		}
		if digested, ok := repoNamed.(reference.Digested); ok {
			return currentImageInfo{
				Tag:          base.Tag,
				Digest:       string(digested.Digest()),
				ImageRef:     repoNamed.Name(),
				ImageID:      base.ImageID,
				DigestPinned: base.DigestPinned,
			}
		}
	}
	return base
}

// RunStreamProducer shares one version poll loop across every connected client and emits a snapshot whenever it changes.
func (s *VersionService) RunStreamProducer(ctx context.Context, events chan<- version.StreamEvent) {
	s.streamHub.Subscribe(ctx, "local",
		func(runCtx context.Context, publish func(version.StreamEvent)) {
			var last *version.Info
			poll := func() {
				info := s.GetAppVersionInfo(runCtx)
				if runCtx.Err() != nil {
					return
				}
				if last != nil {
					if last.UpdateAvailable == info.UpdateAvailable &&
						last.NewestVersion == info.NewestVersion &&
						last.NewestDigest == info.NewestDigest &&
						last.CurrentTag == info.CurrentTag &&
						last.CurrentDigest == info.CurrentDigest &&
						last.ReleaseURL == info.ReleaseURL &&
						last.ReleaseNotes == info.ReleaseNotes &&
						last.ReleasedAt == info.ReleasedAt {
						return
					}
				}
				last = info
				publish(version.StreamEvent{Type: "snapshot", Info: info, Timestamp: time.Now()})
			}

			poll()

			ticker := time.NewTicker(versionStreamPollInterval)
			defer ticker.Stop()

			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
					poll()
				}
			}
		},
		func(event version.StreamEvent) bool {
			return agg.Send(ctx, events, event)
		})
}
