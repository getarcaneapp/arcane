package releasenotes

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/registry"
)

const testDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type testGitHub struct {
	status   int
	releases []githubRelease
}

func newTestDockerService(t *testing.T, labels map[string]string) *docker.DockerClientService {
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
	return (&docker.DockerClientService{}).WithClient(cli)
}

func newTestService(t *testing.T, labels map[string]string, remoteLabels func(context.Context, string) (map[string]string, error), github testGitHub) (*Service, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		if github.status != http.StatusOK {
			w.WriteHeader(github.status)
			return
		}
		assert.NoError(t, json.MarshalWrite(w, github.releases))
	}))
	t.Cleanup(server.Close)

	svc := NewService(newTestDockerService(t, labels), remoteLabels)
	svc.githubAPIBase = server.URL
	return svc, &hits
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

			notes, err := svc.ReleaseNotes(t.Context(), Request{ImageRef: "nginx:1.2.3"})
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, notes.Status)
			assert.Equal(t, tt.wantSource, notes.SourceURL)
			assert.Zero(t, hits.Load())
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
				releases: []githubRelease{{
					TagName:     tt.tag,
					Name:        "Release " + tt.tag,
					Body:        "## Changes",
					HTMLURL:     "https://github.com/o/r/releases/tag/" + tt.tag,
					PublishedAt: "2026-10-01T00:00:00Z",
				}},
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
		releases: []githubRelease{{TagName: "v1.2.3", HTMLURL: "https://github.com/o/r/releases/tag/v1.2.3"}},
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
	assert.Equal(t, int32(1), hits.Load())
}

func TestService_ReleaseNotesKeepsPrereleaseSuffix(t *testing.T) {
	svc, _ := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "https://github.com/o/r"}, noRemoteLabels, testGitHub{
		status:   http.StatusOK,
		releases: []githubRelease{{TagName: "v2.0.0", HTMLURL: "https://github.com/o/r/releases/tag/v2.0.0"}},
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
	assert.Equal(t, int32(2), hits.Load())
}

func TestService_ReleaseNotesCached(t *testing.T) {
	svc, hits := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "https://github.com/o/r"}, noRemoteLabels, testGitHub{
		status:   http.StatusOK,
		releases: []githubRelease{{TagName: "v1.2.3"}},
	})

	for range 2 {
		notes, err := svc.ReleaseNotes(t.Context(), Request{
			ImageRef: "nginx:1.2.2",
			Version:  "1.2.3",
		})
		require.NoError(t, err)
		assert.Equal(t, statusFound, notes.Status)
	}
	assert.Equal(t, int32(1), hits.Load())
}

func TestService_ReleaseLink(t *testing.T) {
	svc, hits := newTestService(t, map[string]string{registry.OCIImageSourceLabel: "git+https://github.com/o/r.git"}, noRemoteLabels, testGitHub{status: http.StatusOK})

	assert.Equal(t, "https://github.com/o/r/releases", svc.ReleaseLink(t.Context(), "nginx:1.2.2"))
	assert.Zero(t, hits.Load())
}

func noRemoteLabels(context.Context, string) (map[string]string, error) {
	return nil, nil
}
