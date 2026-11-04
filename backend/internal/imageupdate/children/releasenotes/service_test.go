package releasenotes

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/docker/streams/bus"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/registry"
	"getarcane.dev/arcane/v3/internal/settings"
)

type (
	testGitHub struct {
		releases map[string][]githubRelease
		search   []string
		status   int
	}

	testRecorder struct {
		authorization atomic.Value
		settings      *settings.SettingsService
		hits          atomic.Int32
	}
)

const (
	testDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func newTestDockerService(t *testing.T, labels map[string]string) (*docker.DockerClientService, *settings.SettingsService) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if labels == nil || !strings.Contains(r.URL.Path, "/images/") || !strings.HasSuffix(r.URL.Path, "/json") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No such image"}`))
			return
		}
		assert.NoError(t, json.MarshalWrite(w, map[string]any{
			"Id":     "sha256:local-image-id",
			"Config": map[string]any{"Labels": labels},
		}))
	}))
	t.Cleanup(server.Close)

	cli, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	return newSettingsDockerService(t, cli)
}

func newTestService(t *testing.T, labels map[string]string, remoteLabels func(context.Context, string) (map[string]string, error), github testGitHub) (*Service, *testRecorder) {
	t.Helper()
	recorder := &testRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.hits.Add(1)
		recorder.authorization.Store(r.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		assert.Equal(t, githubAPIVersion, r.Header.Get("X-GitHub-Api-Version"))
		if github.status != http.StatusOK {
			w.WriteHeader(github.status)
			return
		}
		serveTestGitHub(t, w, r, github)
	}))
	t.Cleanup(server.Close)

	dockerService, settingsService := newTestDockerService(t, labels)
	recorder.settings = settingsService
	svc := NewService(dockerService, settingsService, remoteLabels)
	svc.githubAPIBase = server.URL
	return svc, recorder
}

func serveTestGitHub(t *testing.T, w http.ResponseWriter, r *http.Request, github testGitHub) {
	t.Helper()
	if r.URL.Path == "/search/repositories" {
		items := make([]map[string]any, 0, len(github.search))
		for _, fullName := range github.search {
			_, name, _ := strings.Cut(fullName, "/")
			items = append(items, map[string]any{"full_name": fullName, "name": name})
		}
		assert.NoError(t, json.MarshalWrite(w, map[string]any{"items": items}))
		return
	}

	repoPath, tag, isTagLookup := strings.Cut(strings.TrimPrefix(r.URL.Path, "/repos/"), "/releases/tags/")
	releases, ok := github.releases[strings.TrimSuffix(repoPath, "/releases")]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if !isTagLookup {
		assert.NoError(t, json.MarshalWrite(w, releases[:min(len(releases), releasesPerPage)]))
		return
	}
	index := slices.IndexFunc(releases, func(release githubRelease) bool {
		return release.TagName == tag
	})
	if index < 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	assert.NoError(t, json.MarshalWrite(w, releases[index]))
}

func TestService_ReleaseNotesSourceParsing(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantStatus string
		wantSource string
	}{
		{name: "https", source: "https://github.com/o/r", wantStatus: statusNotFound, wantSource: "https://github.com/o/r"},
		{name: "git suffix", source: "https://github.com/o/r.git", wantStatus: statusNotFound, wantSource: "https://github.com/o/r"},
		{name: "trailing slash", source: "https://github.com/o/r/", wantStatus: statusNotFound, wantSource: "https://github.com/o/r"},
		{name: "http", source: "http://github.com/o/r", wantStatus: statusNotFound, wantSource: "https://github.com/o/r"},
		{name: "tree path", source: "https://github.com/o/r/tree/main", wantStatus: statusNotFound, wantSource: "https://github.com/o/r"},
		{name: "git plus", source: "git+https://github.com/o/r.git", wantStatus: statusNotFound, wantSource: "https://github.com/o/r"},
		{name: "www", source: "https://www.github.com/o/r", wantStatus: statusNotFound, wantSource: "https://github.com/o/r"},
		{name: "port", source: "https://github.com:443/o/r", wantStatus: statusNotFound, wantSource: "https://github.com/o/r"},
		{name: "gitlab", source: "https://gitlab.com/o/r", wantStatus: statusLink, wantSource: "https://gitlab.com/o/r"},
		{name: "bad owner", source: "https://github.com/-o/r", wantStatus: statusNoSource},
		{name: "javascript", source: "javascript:alert(1)", wantStatus: statusNoSource},
		{name: "markup characters", source: "https://evil.example/?x|Click>", wantStatus: statusNoSource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, hits := newTestService(t, map[string]string{registry.OCIImageSourceLabel: tt.source}, noRemoteLabels, testGitHub{status: http.StatusOK})

			notes, err := svc.ReleaseNotes(t.Context(), Request{ImageRef: "nginx:latest"})
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, notes.Status)
			assert.Equal(t, tt.wantSource, notes.SourceURL)
			assert.Zero(t, hits.hits.Load())
		})
	}
}

func TestService_ReleaseNotesVersionMatching(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		tag        string
		wantStatus string
	}{
		{name: "adds v prefix", version: "1.2.3", tag: "v1.2.3", wantStatus: statusFound},
		{name: "drops v prefix", version: "v1.2.3", tag: "1.2.3", wantStatus: statusFound},
		{name: "drops variant suffix", version: "1.2.3-alpine", tag: "1.2.3", wantStatus: statusFound},
		{name: "miss", version: "9.9.9", tag: "v1.2.3", wantStatus: statusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "https://github.com/o/r"}, noRemoteLabels, testGitHub{
				status: http.StatusOK,
				releases: map[string][]githubRelease{"o/r": {{
					TagName:     tt.tag,
					Name:        "Release " + tt.tag,
					Body:        "## Changes",
					HTMLURL:     "https://github.com/o/r/releases/tag/" + tt.tag,
					PublishedAt: "2026-10-01T00:00:00Z",
				}}},
			})

			notes, err := svc.ReleaseNotes(t.Context(), Request{
				ImageRef: "nginx:1.2.2",
				Version:  tt.version,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, notes.Status)
			if tt.wantStatus == statusFound {
				assert.Equal(t, "Release "+tt.tag, notes.ReleaseName)
				assert.Equal(t, "## Changes", notes.Notes)
				assert.Equal(t, "https://github.com/o/r/releases/tag/"+tt.tag, notes.ReleaseURL)
				return
			}
			assert.True(t, strings.HasSuffix(notes.ReleaseURL, "/releases"))
		})
	}
}

func TestService_ReleaseNotesDigestResolvesVersion(t *testing.T) {
	svc, _ := newTestService(t, nil, func(_ context.Context, ref string) (map[string]string, error) {
		if ref != "docker.io/library/nginx@"+testDigest {
			return nil, errors.New("unexpected reference " + ref)
		}
		return map[string]string{
			registry.OCIImageVersionLabel: "1.2.3",
			registry.OCIImageSourceLabel:  "https://github.com/o/r",
		}, nil
	}, testGitHub{
		status:   http.StatusOK,
		releases: map[string][]githubRelease{"o/r": {{TagName: "v1.2.3", HTMLURL: "https://github.com/o/r/releases/tag/v1.2.3"}}},
	})

	notes, err := svc.ReleaseNotes(t.Context(), Request{
		ImageRef: "nginx:1.2.2",
		Digest:   testDigest,
	})
	require.NoError(t, err)
	assert.Equal(t, statusFound, notes.Status)
	assert.Equal(t, "1.2.3", notes.Version)
}

func TestService_ReleaseNotesRejectsInvalidDigest(t *testing.T) {
	svc, _ := newTestService(t, nil, noRemoteLabels, testGitHub{status: http.StatusOK})

	_, err := svc.ReleaseNotes(t.Context(), Request{
		ImageRef: "nginx:1.2.2",
		Digest:   "sha256:abc",
	})
	require.Error(t, err)
}

func TestService_ReleaseNotesGitHubNotFoundIsCached(t *testing.T) {
	svc, hits := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "https://github.com/orgs/foo"}, noRemoteLabels, testGitHub{status: http.StatusNotFound})

	for range 2 {
		notes, err := svc.ReleaseNotes(t.Context(), Request{
			ImageRef: "nginx:1.2.2",
			Version:  "1.2.3",
		})
		require.NoError(t, err)
		assert.Equal(t, statusNotFound, notes.Status)
		assert.Equal(t, "https://github.com/orgs/foo/releases", notes.ReleaseURL)
	}
	assert.Equal(t, int32(2), hits.hits.Load())
}

func TestService_ReleaseNotesKeepsPrereleaseSuffix(t *testing.T) {
	svc, _ := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "https://github.com/o/r"}, noRemoteLabels, testGitHub{
		status:   http.StatusOK,
		releases: map[string][]githubRelease{"o/r": {{TagName: "v2.0.0", HTMLURL: "https://github.com/o/r/releases/tag/v2.0.0"}}},
	})

	notes, err := svc.ReleaseNotes(t.Context(), Request{
		ImageRef: "nginx:1.2.2",
		Version:  "2.0.0-rc.1",
	})
	require.NoError(t, err)
	assert.Equal(t, statusNotFound, notes.Status)
}

func TestService_ReleaseNotesWithoutLabels(t *testing.T) {
	svc, _ := newTestService(t, nil, noRemoteLabels, testGitHub{status: http.StatusOK})

	notes, err := svc.ReleaseNotes(t.Context(), Request{
		ImageRef: "nginx:1.2.2",
		Version:  "1.2.3",
	})
	require.NoError(t, err)
	assert.Equal(t, statusNoSource, notes.Status)
}

func TestService_ReleaseNotesUnavailableNotCached(t *testing.T) {
	svc, hits := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "https://github.com/o/r"}, noRemoteLabels, testGitHub{status: http.StatusForbidden})

	for range 2 {
		notes, err := svc.ReleaseNotes(t.Context(), Request{
			ImageRef: "nginx:1.2.2",
			Version:  "1.2.3",
		})
		require.NoError(t, err)
		assert.Equal(t, statusUnavailable, notes.Status)
		assert.Equal(t, "https://github.com/o/r/releases", notes.ReleaseURL)
	}
	assert.Equal(t, int32(2), hits.hits.Load())
}

func TestService_ReleaseNotesCached(t *testing.T) {
	svc, hits := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "https://github.com/o/r"}, noRemoteLabels, testGitHub{
		status:   http.StatusOK,
		releases: map[string][]githubRelease{"o/r": {{TagName: "v1.2.3"}}},
	})

	for range 2 {
		notes, err := svc.ReleaseNotes(t.Context(), Request{
			ImageRef: "nginx:1.2.2",
			Version:  "1.2.3",
		})
		require.NoError(t, err)
		assert.Equal(t, statusFound, notes.Status)
	}
	assert.Equal(t, int32(1), hits.hits.Load())
}

func TestService_ReleaseLink(t *testing.T) {
	svc, hits := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "git+https://github.com/o/r.git"}, noRemoteLabels, testGitHub{status: http.StatusOK})

	assert.Equal(t, "https://github.com/o/r/releases", svc.ReleaseLink(t.Context(), "nginx:1.2.2"))
	assert.Zero(t, hits.hits.Load())
}

func TestService_ReleaseNotesPrefixedTags(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantTag string
		tags    []string
	}{
		{name: "release prefix", version: "5.3.0", tags: []string{"release-5.3.1", "release-5.3.0"}, wantTag: "release-5.3.0"},
		{name: "app prefix", version: "v2.1.0", tags: []string{"app-v2.1.0"}, wantTag: "app-v2.1.0"},
		{name: "prerelease tag not matched", version: "5.3.0", tags: []string{"release-5.3.0rc1"}},
		{name: "prefers image named tag", version: "1.0.0", tags: []string{"server-1.0.0", "nginx-1.0.0"}, wantTag: "nginx-1.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			releases := make([]githubRelease, 0, len(tt.tags))
			for _, tag := range tt.tags {
				releases = append(releases, githubRelease{TagName: tag, HTMLURL: "https://github.com/o/r/releases/tag/" + tag})
			}
			svc, _ := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "https://github.com/o/r"}, noRemoteLabels, testGitHub{
				status:   http.StatusOK,
				releases: map[string][]githubRelease{"o/r": releases},
			})

			notes, err := svc.ReleaseNotes(t.Context(), Request{
				ImageRef: "nginx:1.0.0",
				Version:  tt.version,
			})
			require.NoError(t, err)
			if tt.wantTag == "" {
				assert.Equal(t, statusNotFound, notes.Status)
				return
			}
			assert.Equal(t, statusFound, notes.Status)
			assert.Equal(t, "https://github.com/o/r/releases/tag/"+tt.wantTag, notes.ReleaseURL)
		})
	}
}

func TestService_ReleaseNotesTagLookupBeyondFirstPage(t *testing.T) {
	releases := make([]githubRelease, 0, releasesPerPage+1)
	for i := range releasesPerPage {
		releases = append(releases, githubRelease{TagName: "v2.0." + strconv.Itoa(i)})
	}
	releases = append(releases, githubRelease{
		TagName: "v1.2.3",
		Name:    "Old release",
		HTMLURL: "https://github.com/o/r/releases/tag/v1.2.3",
	})
	svc, hits := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "https://github.com/o/r"}, noRemoteLabels, testGitHub{
		status:   http.StatusOK,
		releases: map[string][]githubRelease{"o/r": releases},
	})

	for range 2 {
		notes, err := svc.ReleaseNotes(t.Context(), Request{
			ImageRef: "nginx:1.2.2",
			Version:  "1.2.3",
		})
		require.NoError(t, err)
		assert.Equal(t, statusFound, notes.Status)
		assert.Equal(t, "Old release", notes.ReleaseName)
	}
	assert.Equal(t, int32(3), hits.hits.Load())
}

func TestService_ReleaseNotesDiscoversUpstreamRepo(t *testing.T) {
	svc, _ := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "https://github.com/hotio/qbittorrent"}, noRemoteLabels, testGitHub{
		status: http.StatusOK,
		search: []string{"hotio/qbittorrent", "qbittorrent/qBittorrent"},
		releases: map[string][]githubRelease{
			"hotio/qbittorrent": {},
			"qbittorrent/qBittorrent": {{
				TagName: "release-5.3.0",
				Name:    "qBittorrent v5.3.0",
				Body:    "## Changes",
				HTMLURL: "https://github.com/qbittorrent/qBittorrent/releases/tag/release-5.3.0",
			}},
		},
	})

	notes, err := svc.ReleaseNotes(t.Context(), Request{
		ImageRef: "ghcr.io/hotio/qbittorrent:release",
		Version:  "5.3.0",
	})
	require.NoError(t, err)
	assert.Equal(t, statusFound, notes.Status)
	assert.Equal(t, "https://github.com/hotio/qbittorrent", notes.SourceURL)
	assert.Equal(t, "https://github.com/qbittorrent/qBittorrent/releases/tag/release-5.3.0", notes.ReleaseURL)
	assert.Equal(t, "## Changes", notes.Notes)
}

func TestService_ReleaseNotesDiscoversRepoWithoutLabels(t *testing.T) {
	svc, _ := newTestService(t, nil, noRemoteLabels, testGitHub{
		status: http.StatusOK,
		search: []string{"nginx/nginx"},
		releases: map[string][]githubRelease{
			"nginx/nginx": {{TagName: "release-1.27.3", HTMLURL: "https://github.com/nginx/nginx/releases/tag/release-1.27.3"}},
		},
	})

	notes, err := svc.ReleaseNotes(t.Context(), Request{
		ImageRef: "nginx:1.27.2",
		Version:  "1.27.3",
	})
	require.NoError(t, err)
	assert.Equal(t, statusFound, notes.Status)
	assert.Equal(t, "https://github.com/nginx/nginx", notes.SourceURL)
}

func TestService_ReleaseNotesRejectsUnverifiedUpstream(t *testing.T) {
	svc, _ := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "https://github.com/hotio/qbittorrent"}, noRemoteLabels, testGitHub{
		status: http.StatusOK,
		search: []string{"lookalike/qbittorrent", "qbittorrent/qBittorrent"},
		releases: map[string][]githubRelease{
			"hotio/qbittorrent":       {},
			"lookalike/qbittorrent":   {{TagName: "5.3.0", HTMLURL: "https://github.com/lookalike/qbittorrent/releases/tag/5.3.0"}},
			"qbittorrent/qBittorrent": {{TagName: "release-5.2.0"}},
		},
	})

	notes, err := svc.ReleaseNotes(t.Context(), Request{
		ImageRef: "ghcr.io/hotio/qbittorrent:release",
		Version:  "5.3.0",
	})
	require.NoError(t, err)
	assert.Equal(t, statusNotFound, notes.Status)
	assert.Equal(t, "https://github.com/hotio/qbittorrent/releases", notes.ReleaseURL)
}

func TestService_ReleaseNotesSendsGitHubToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{name: "without token"},
		{name: "with token", token: "ghp_test", want: "Bearer ghp_test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, recorder := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "https://github.com/o/r"}, noRemoteLabels, testGitHub{
				status:   http.StatusOK,
				releases: map[string][]githubRelease{"o/r": {{TagName: "v1.2.3"}}},
			})
			if tt.token != "" {
				crypto.InitEncryption(crypto.Config{EncryptionKey: "release-notes-test-key-32-bytes!", Environment: "test"})
				require.NoError(t, recorder.settings.UpdateSetting(t.Context(), "githubToken", tt.token))
			}

			_, err := svc.ReleaseNotes(t.Context(), Request{
				ImageRef: "nginx:1.2.2",
				Version:  "1.2.3",
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, recorder.authorization.Load())
		})
	}
}

func TestService_ReleaseNotesRunningVersion(t *testing.T) {
	tests := []struct {
		name        string
		labels      map[string]string
		imageRef    string
		wantStatus  string
		wantVersion string
	}{
		{
			name:        "local version label",
			labels:      map[string]string{registry.OCIImageSourceLabel: "https://github.com/o/r", registry.OCIImageVersionLabel: "1.2.3"},
			imageRef:    "ghcr.io/o/r:release",
			wantStatus:  statusFound,
			wantVersion: "1.2.3",
		},
		{
			name:        "numeric tag",
			labels:      map[string]string{registry.OCIImageSourceLabel: "https://github.com/o/r"},
			imageRef:    "ghcr.io/o/r:1.2.3",
			wantStatus:  statusFound,
			wantVersion: "1.2.3",
		},
		{
			name:       "non version tag",
			labels:     map[string]string{registry.OCIImageSourceLabel: "https://github.com/o/r"},
			imageRef:   "ghcr.io/o/r:latest",
			wantStatus: statusNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestService(t, tt.labels, noRemoteLabels, testGitHub{
				status:   http.StatusOK,
				releases: map[string][]githubRelease{"o/r": {{TagName: "v1.2.3"}}},
			})

			notes, err := svc.ReleaseNotes(t.Context(), Request{ImageRef: tt.imageRef})
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, notes.Status)
			assert.Equal(t, tt.wantVersion, notes.Version)
		})
	}
}

func noRemoteLabels(context.Context, string) (map[string]string, error) {
	return nil, nil
}

func newSettingsDockerService(t *testing.T, dockerClient *client.Client) (*docker.DockerClientService, *settings.SettingsService) {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}))
	db := &database.DB{DB: gormDB}
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     db,
		Config: &config.Config{},
		Env:    settings.Env{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	return docker.NewDockerClientService(t.Context(), db, settingsService, bus.Config{}).WithClient(dockerClient), settingsService
}
