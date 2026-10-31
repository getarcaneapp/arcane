package releasenotes

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/client"
	"github.com/opencontainers/go-digest"
	"github.com/samber/hot"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

const (
	githubAPIBase     = "https://api.github.com"
	githubURLBase     = "https://github.com/"
	maxReleasesBytes  = 8 << 20
	statusFound       = "found"
	statusNotFound    = "not_found"
	statusUnavailable = "unavailable"
	statusLink        = "link"
	statusNoSource    = "no_source"
)

var (
	githubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$`)
	prereleasePattern = regexp.MustCompile(`(?i)^(rc|alpha|beta|pre|next|dev|canary|nightly|snapshot)`)
	githubHosts       = []string{"github.com", "www.github.com"}
)

type Service struct {
	docker        *docker.DockerClientService
	remoteLabels  func(context.Context, string) (map[string]string, error)
	httpClient    *http.Client
	releases      *hot.HotCache[string, []githubRelease]
	githubAPIBase string
}

type Request struct {
	ImageRef string
	Version  string
	Digest   string
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
}

func NewService(dockerService *docker.DockerClientService, remoteLabels func(context.Context, string) (map[string]string, error)) *Service {
	return &Service{
		docker:       dockerService,
		remoteLabels: remoteLabels,
		httpClient: httpx.NewHTTPClient(types.HTTPClientOptions{
			Timeout:             timeouts.DefaultHTTPClient,
			TLSHandshakeTimeout: 10 * time.Second,
		}),
		githubAPIBase: githubAPIBase,
		releases:      hot.NewHotCache[string, []githubRelease](hot.LRU, 256).WithTTL(time.Hour).Build(),
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
		labels, labelsErr := s.remoteLabels(ctx, named.Name()+"@"+strings.TrimSpace(req.Digest))
		if labelsErr != nil {
			slog.WarnContext(ctx, "Failed to resolve remote image labels for release notes", "imageRef", imageRef, "digest", req.Digest, "error", labelsErr)
		}
		return labels
	}()
	version := cmp.Or(strings.TrimSpace(req.Version), strings.TrimSpace(digestLabels[registry.OCIImageVersionLabel]))

	inspected, inspectErr := s.docker.Call(ctx, func(ctx context.Context, c *client.Client) (client.ImageInspectResult, error) {
		return c.ImageInspect(ctx, imageRef)
	})
	if inspectErr != nil {
		slog.DebugContext(ctx, "Failed to inspect local image for release notes", "imageRef", imageRef, "error", inspectErr)
	}
	source := cmp.Or(sourceLabel(kit.FromPtr(inspected.Config).Labels), sourceLabel(digestLabels))
	if source == "" && version != "" {
		labels, labelsErr := s.remoteLabels(ctx, named.Name()+":"+version)
		if labelsErr != nil {
			slog.WarnContext(ctx, "Failed to resolve remote image labels for release notes", "imageRef", imageRef, "version", version, "error", labelsErr)
		}
		source = sourceLabel(labels)
	}

	repo, isGitHub := parseGitHubRepo(source)
	if !isGitHub {
		link := externalLink(source)
		return &types.ImageReleaseNotes{
			ImageRef:   imageRef,
			Version:    version,
			Status:     kit.Ternary(link != "", statusLink, statusNoSource),
			SourceURL:  link,
			ReleaseURL: link,
		}, nil
	}
	if version == "" {
		return &types.ImageReleaseNotes{
			ImageRef:   imageRef,
			Status:     statusNotFound,
			SourceURL:  githubURLBase + repo,
			ReleaseURL: githubURLBase + repo + "/releases",
		}, nil
	}

	releases, releasesErr := s.githubReleases(ctx, repo)
	if releasesErr != nil {
		slog.WarnContext(ctx, "Failed to fetch GitHub releases", "repo", repo, "error", releasesErr)
	}
	release, found := matchRelease(releases, version)
	if !found {
		return &types.ImageReleaseNotes{
			ImageRef:   imageRef,
			Version:    version,
			Status:     kit.Ternary(releasesErr != nil, statusUnavailable, statusNotFound),
			SourceURL:  githubURLBase + repo,
			ReleaseURL: githubURLBase + repo + "/releases",
		}, nil
	}
	return &types.ImageReleaseNotes{
		ImageRef:    imageRef,
		Version:     version,
		Status:      statusFound,
		SourceURL:   githubURLBase + repo,
		ReleaseURL:  release.HTMLURL,
		ReleaseName: release.Name,
		Notes:       release.Body,
		PublishedAt: release.PublishedAt,
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

func (s *Service) githubReleases(ctx context.Context, repo string) ([]githubRelease, error) {
	releases, _, err := s.releases.GetWithLoaders(repo, func(_ []string) (map[string][]githubRelease, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultHTTPClient)
		defer cancel()

		req, err := http.NewRequestWithContext(loadCtx, http.MethodGet, s.githubAPIBase+"/repos/"+repo+"/releases?per_page=50", http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("create GitHub releases request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch GitHub releases: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == http.StatusNotFound {
			return map[string][]githubRelease{repo: {}}, nil
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		}

		var fetched []githubRelease
		if unmarshalReadErr := json.UnmarshalRead(io.LimitReader(resp.Body, maxReleasesBytes), &fetched); unmarshalReadErr != nil {
			return nil, fmt.Errorf("decode GitHub releases: %w", unmarshalReadErr)
		}
		return map[string][]githubRelease{
			repo: slices.DeleteFunc(fetched, func(release githubRelease) bool {
				return release.Draft
			}),
		}, nil
	})
	return releases, err
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

func matchRelease(releases []githubRelease, version string) (githubRelease, bool) {
	base, suffix, _ := strings.Cut(version, "-")
	for _, value := range []string{version, kit.Ternary(prereleasePattern.MatchString(suffix), version, base)} {
		for _, candidate := range []string{value, "v" + strings.TrimPrefix(value, "v"), strings.TrimPrefix(value, "v")} {
			index := slices.IndexFunc(releases, func(release githubRelease) bool {
				return strings.EqualFold(release.TagName, candidate)
			})
			if index >= 0 {
				return releases[index], true
			}
		}
	}
	return githubRelease{}, false
}
