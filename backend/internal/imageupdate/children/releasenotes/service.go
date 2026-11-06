package releasenotes

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/distribution/reference"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/moby/client"
	"github.com/opencontainers/go-digest"
	"github.com/samber/hot"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/registry"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/types/v3"
)

type (
	Service struct {
		docker        *docker.DockerClientService
		settings      *settings.SettingsService
		remoteConfig  func(context.Context, string, registry.ImageConfigOptions) (*v1.ConfigFile, error)
		httpClient    *http.Client
		releases      *hot.HotCache[string, []githubRelease]
		taggedRelease *hot.HotCache[string, githubRelease]
		upstreamRepos *hot.HotCache[string, []string]
		githubAPIBase string
	}

	Request struct {
		ImageRef string
		Version  string
		Digest   string
	}

	githubRelease struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		Body        string `json:"body"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
		Draft       bool   `json:"draft"`
	}

	findRequest struct {
		labeledRepo string
		imagePath   string
		version     string
	}

	githubRepoSearch struct {
		Items []struct {
			FullName string `json:"full_name"`
			Name     string `json:"name"`
			Fork     bool   `json:"fork"`
		} `json:"items"`
	}
)

const (
	githubAPIBase      = "https://api.github.com"
	githubAPIVersion   = "2022-11-28"
	githubURLBase      = "https://github.com/"
	maxReleasesBytes   = 8 << 20
	releasesPerPage    = 50
	maxUpstreamRepos   = 3
	statusFound        = "found"
	statusNotFound     = "not_found"
	statusUnavailable  = "unavailable"
	statusLink         = "link"
	statusNoSource     = "no_source"
	releaseTagPrefix   = "release-"
	rateLimitRemaining = "X-RateLimit-Remaining"
	rateLimitReset     = "X-RateLimit-Reset"
)

var (
	githubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$`)
	prereleasePattern = regexp.MustCompile(`(?i)^(rc|alpha|beta|pre|next|dev|canary|nightly|snapshot)`)
	githubHosts       = []string{"github.com", "www.github.com"}
)

func NewService(
	dockerService *docker.DockerClientService,
	settingsService *settings.SettingsService,
	remoteConfig func(context.Context, string, registry.ImageConfigOptions) (*v1.ConfigFile, error),
) *Service {
	return &Service{
		docker:       dockerService,
		settings:     settingsService,
		remoteConfig: remoteConfig,
		httpClient: httpx.NewHTTPClient(httpx.ClientConfig{
			Timeout:             timeouts.DefaultHTTPClient,
			MaxIdleConnsPerHost: 32,
		}),
		githubAPIBase: githubAPIBase,
		releases:      hot.NewHotCache[string, []githubRelease](hot.LRU, 256).WithTTL(time.Hour).Build(),
		taggedRelease: hot.NewHotCache[string, githubRelease](hot.LRU, 256).WithTTL(time.Hour).Build(),
		upstreamRepos: hot.NewHotCache[string, []string](hot.LRU, 256).WithTTL(time.Hour).Build(),
	}
}

func (s *Service) ReleaseNotes(ctx context.Context, req Request) (*types.ImageReleaseNotes, error) {
	imageRef := strings.TrimSpace(req.ImageRef)
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return nil, common.Classify(common.ErrBadRequest, err)
	}
	if strings.TrimSpace(req.Digest) != "" {
		if _, digestErr := digest.Parse(strings.TrimSpace(req.Digest)); digestErr != nil {
			return nil, common.Classify(common.ErrBadRequest, digestErr)
		}
	}

	digestLabels := func() map[string]string {
		if strings.TrimSpace(req.Version) != "" || strings.TrimSpace(req.Digest) == "" {
			return nil
		}
		cfg, cfgErr := s.remoteConfig(ctx, named.Name()+"@"+strings.TrimSpace(req.Digest), registry.ImageConfigOptions{})
		if cfgErr != nil {
			slog.WarnContext(ctx, "Failed to resolve remote image labels for release notes", "imageRef", imageRef, "digest", req.Digest, "error", cfgErr)
		}
		return kit.FromPtr(cfg).Config.Labels
	}()

	inspected, inspectErr := s.docker.Call(ctx, func(ctx context.Context, c *client.Client) (client.ImageInspectResult, error) {
		return c.ImageInspect(ctx, imageRef)
	})
	if inspectErr != nil {
		slog.DebugContext(ctx, "Failed to inspect local image for release notes", "imageRef", imageRef, "error", inspectErr)
	}
	localLabels := kit.FromPtr(inspected.Config).Labels
	version := cmp.Or(
		strings.TrimSpace(req.Version),
		strings.TrimSpace(digestLabels[registry.OCIImageVersionLabel]),
		kit.Ternary(strings.TrimSpace(req.Digest) == "", cmp.Or(strings.TrimSpace(localLabels[registry.OCIImageVersionLabel]), versionTag(named)), ""),
	)
	source := cmp.Or(sourceLabel(localLabels), sourceLabel(digestLabels))
	if source == "" && version != "" {
		cfg, cfgErr := s.remoteConfig(ctx, named.Name()+":"+version, registry.ImageConfigOptions{})
		if cfgErr != nil {
			slog.WarnContext(ctx, "Failed to resolve remote image labels for release notes", "imageRef", imageRef, "version", version, "error", cfgErr)
		}
		source = sourceLabel(kit.FromPtr(cfg).Config.Labels)
	}

	repo, isGitHub := parseGitHubRepo(source)
	link := externalLink(source)
	release, releaseRepo, lookupErr := s.findRelease(ctx, findRequest{
		labeledRepo: kit.Ternary(isGitHub, repo, ""),
		imagePath:   reference.Path(named),
		version:     version,
	})
	if lookupErr != nil {
		slog.WarnContext(ctx, "Failed to fetch GitHub release notes", "imageRef", imageRef, "repo", cmp.Or(releaseRepo, repo), "error", lookupErr)
	}
	if release.TagName != "" {
		return &types.ImageReleaseNotes{
			ImageRef:    imageRef,
			Version:     version,
			Status:      statusFound,
			SourceURL:   kit.Ternary(isGitHub, githubURLBase+repo, cmp.Or(link, githubURLBase+releaseRepo)),
			ReleaseURL:  release.HTMLURL,
			ReleaseName: release.Name,
			Notes:       release.Body,
			PublishedAt: release.PublishedAt,
		}, nil
	}
	if !isGitHub {
		return &types.ImageReleaseNotes{
			ImageRef:   imageRef,
			Version:    version,
			Status:     kit.Ternary(link != "", statusLink, statusNoSource),
			SourceURL:  link,
			ReleaseURL: link,
		}, nil
	}
	return &types.ImageReleaseNotes{
		ImageRef:   imageRef,
		Version:    version,
		Status:     kit.Ternary(lookupErr != nil, statusUnavailable, statusNotFound),
		SourceURL:  githubURLBase + repo,
		ReleaseURL: githubURLBase + repo + "/releases",
	}, nil
}

func (s *Service) ReleaseLink(ctx context.Context, imageRef string) string {
	inspected, err := s.docker.Call(ctx, func(ctx context.Context, c *client.Client) (client.ImageInspectResult, error) {
		return c.ImageInspect(ctx, strings.TrimSpace(imageRef))
	})
	if err != nil {
		slog.DebugContext(ctx, "Failed to inspect local image for release link", "imageRef", imageRef, "error", err)
	}
	source := sourceLabel(kit.FromPtr(inspected.Config).Labels)
	if repo, ok := parseGitHubRepo(source); ok {
		return githubURLBase + repo + "/releases"
	}
	return externalLink(source)
}

func (s *Service) findRelease(ctx context.Context, req findRequest) (githubRelease, string, error) {
	if req.version == "" {
		return githubRelease{}, "", nil
	}
	imageName := path.Base(req.imagePath)
	if req.labeledRepo != "" {
		release, err := s.repoRelease(ctx, req.labeledRepo, req.version, imageName)
		if err != nil || release.TagName != "" {
			return release, req.labeledRepo, err
		}
	}

	candidates, err := s.searchUpstreamRepos(ctx, req.imagePath)
	if err != nil {
		return githubRelease{}, "", err
	}
	for _, repo := range candidates {
		if strings.EqualFold(repo, req.labeledRepo) {
			continue
		}
		release, releaseErr := s.repoRelease(ctx, repo, req.version, imageName)
		if releaseErr != nil || release.TagName != "" {
			return release, repo, releaseErr
		}
	}
	return githubRelease{}, "", nil
}

func (s *Service) repoRelease(ctx context.Context, repo, version, imageName string) (githubRelease, error) {
	releases, err := s.githubReleases(ctx, repo)
	if err != nil {
		return githubRelease{}, err
	}
	if release, found := matchRelease(releases, version, imageName); found || len(releases) < releasesPerPage {
		return release, nil
	}

	key := repo + "@" + version
	release, _, err := s.taggedRelease.GetWithLoaders(key, func(_ []string) (map[string]githubRelease, error) {
		for _, tag := range slices.Compact([]string{version, "v" + strings.TrimPrefix(version, "v"), releaseTagPrefix + version}) {
			var tagged githubRelease
			status, fetchErr := s.githubGet(ctx, "/repos/"+repo+"/releases/tags/"+url.PathEscape(tag), nil, &tagged)
			if fetchErr != nil {
				return nil, fetchErr
			}
			if status == http.StatusOK && !tagged.Draft {
				return map[string]githubRelease{key: tagged}, nil
			}
		}
		return map[string]githubRelease{key: {}}, nil
	})
	return release, err
}

func (s *Service) searchUpstreamRepos(ctx context.Context, imagePath string) ([]string, error) {
	imageName := path.Base(imagePath)
	owners := []string{imageName, path.Base(path.Dir(imagePath))}
	repos, _, err := s.upstreamRepos.GetWithLoaders(strings.ToLower(imagePath), func(keys []string) (map[string][]string, error) {
		var result githubRepoSearch
		if _, fetchErr := s.githubGet(ctx, "/search/repositories", url.Values{
			"q":        {imageName + " in:name"},
			"sort":     {"stars"},
			"order":    {"desc"},
			"per_page": {"10"},
		}, &result); fetchErr != nil {
			return nil, fetchErr
		}

		var found []string
		for _, item := range result.Items {
			owner, _, _ := strings.Cut(item.FullName, "/")
			if !item.Fork && strings.EqualFold(item.Name, imageName) && slices.ContainsFunc(owners, func(candidate string) bool {
				return strings.EqualFold(candidate, owner)
			}) && githubRepoPattern.MatchString(item.FullName) && len(found) < maxUpstreamRepos {
				found = append(found, item.FullName)
			}
		}
		return map[string][]string{keys[0]: found}, nil
	})
	return repos, err
}

func (s *Service) githubReleases(ctx context.Context, repo string) ([]githubRelease, error) {
	releases, _, err := s.releases.GetWithLoaders(repo, func(_ []string) (map[string][]githubRelease, error) {
		var fetched []githubRelease
		if _, fetchErr := s.githubGet(ctx, "/repos/"+repo+"/releases", url.Values{"per_page": {strconv.Itoa(releasesPerPage)}}, &fetched); fetchErr != nil {
			return nil, fetchErr
		}
		return map[string][]githubRelease{
			repo: slices.DeleteFunc(fetched, func(release githubRelease) bool {
				return release.Draft
			}),
		}, nil
	})
	return releases, err
}

func (s *Service) githubGet(ctx context.Context, apiPath string, query url.Values, out any) (int, error) {
	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultHTTPClient)
	defer cancel()

	req, err := http.NewRequestWithContext(loadCtx, http.MethodGet, s.githubAPIBase+apiPath+kit.Ternary(len(query) > 0, "?"+query.Encode(), ""), http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("create GitHub request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	if token := strings.TrimSpace(s.settings.GetSettingsConfig().GithubToken.Value); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch GitHub %s: %w", apiPath, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return resp.StatusCode, nil
	case (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) && resp.Header.Get(rateLimitRemaining) == "0":
		resetUnix, _ := strconv.ParseInt(resp.Header.Get(rateLimitReset), 10, 64)
		slog.WarnContext(ctx, "GitHub API rate limit reached; set a GitHub token to raise it", "resetAt", time.Unix(resetUnix, 0))
		return resp.StatusCode, errors.New("GitHub API rate limit reached")
	case resp.StatusCode != http.StatusOK:
		return resp.StatusCode, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	if decodeErr := json.UnmarshalRead(io.LimitReader(resp.Body, maxReleasesBytes), out); decodeErr != nil {
		return resp.StatusCode, fmt.Errorf("decode GitHub %s: %w", apiPath, decodeErr)
	}
	return resp.StatusCode, nil
}

func sourceLabel(labels map[string]string) string {
	return cmp.Or(
		strings.TrimSpace(labels[registry.OCIImageSourceLabel]),
		strings.TrimSpace(labels[registry.OCIImageURLLabel]),
		strings.TrimSpace(labels[registry.OCIImageDocumentationLabel]),
	)
}

func parseGitHubRepo(raw string) (string, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), "git+")
	parsed, err := url.Parse(kit.Ternary(strings.HasPrefix(trimmed, "git://"), "https://"+strings.TrimPrefix(trimmed, "git://"), trimmed))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || !slices.Contains(githubHosts, strings.ToLower(parsed.Hostname())) {
		return "", false
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 2 || slices.Contains([]string{".", ".."}, strings.TrimSuffix(segments[1], ".git")) {
		return "", false
	}
	repo := segments[0] + "/" + strings.TrimSuffix(segments[1], ".git")
	if !githubRepoPattern.MatchString(repo) {
		return "", false
	}
	return repo, true
}

func externalLink(source string) string {
	parsed, err := httpx.ValidateOutboundHTTPURL(source)
	if err != nil || slices.Contains(githubHosts, strings.ToLower(parsed.Hostname())) || strings.ContainsAny(parsed.String(), "<>|()[] \"'") {
		return ""
	}
	return parsed.String()
}

func matchRelease(releases []githubRelease, version, imageName string) (githubRelease, bool) {
	base, suffix, _ := strings.Cut(version, "-")
	values := slices.Compact([]string{version, kit.Ternary(prereleasePattern.MatchString(suffix), version, base)})
	for _, value := range values {
		for _, candidate := range []string{value, "v" + strings.TrimPrefix(value, "v"), strings.TrimPrefix(value, "v")} {
			index := slices.IndexFunc(releases, func(release githubRelease) bool {
				return strings.EqualFold(release.TagName, candidate)
			})
			if index >= 0 {
				return releases[index], true
			}
		}
	}

	for _, value := range values {
		_, core := splitVersionPrefix(value)
		if core == "" {
			continue
		}
		matches := slices.DeleteFunc(slices.Clone(releases), func(release githubRelease) bool {
			_, tagCore := splitVersionPrefix(release.TagName)
			return !strings.EqualFold(tagCore, core)
		})
		if len(matches) == 0 {
			continue
		}
		preferred := slices.IndexFunc(matches, func(release githubRelease) bool {
			prefix, _ := splitVersionPrefix(release.TagName)
			return strings.Contains(strings.ToLower(prefix), strings.ToLower(imageName))
		})
		return matches[max(preferred, 0)], true
	}
	return githubRelease{}, false
}

func versionTag(named reference.Named) string {
	tagged, ok := named.(reference.Tagged)
	if !ok || !strings.ContainsFunc(tagged.Tag(), unicode.IsDigit) {
		return ""
	}
	return tagged.Tag()
}

func splitVersionPrefix(tag string) (string, string) {
	index := strings.IndexFunc(tag, unicode.IsDigit)
	if index < 0 {
		return tag, ""
	}
	return tag[:index], tag[index:]
}
