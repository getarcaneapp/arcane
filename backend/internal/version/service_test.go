package version

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"

	"github.com/getarcaneapp/arcane/backend/v2/internal/event"

	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	dockertypesimage "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/updater/labels"
	"gorm.io/gorm"
)

func TestVersionService_GetAppVersionInfoDoesNotUseStoredDigestUpdateForSemverBuildInternal(t *testing.T) {
	ctx := context.Background()
	db := setupImageUpdateTestDB(t)

	const (
		containerID = "arcane-container-1234567890"
		imageID     = "sha256:arcane-image"
		imageRef    = "ghcr.io/getarcaneapp/arcane:latest"
	)
	currentDigest := digest.FromString("current-arcane").String()
	latestDigest := digest.FromString("latest-arcane").String()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{
					ID:    containerID,
					State: container.StateRunning,
					Labels: map[string]string{
						labels.LabelArcane: "true",
					},
				},
			})) {
				return
			}
		case strings.Contains(r.URL.Path, "/containers/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(container.InspectResponse{
				ID:    containerID,
				Image: imageID,
				Config: &container.Config{
					Image: imageRef,
					Labels: map[string]string{
						labels.LabelArcane: "true",
					},
				},
			})) {
				return
			}
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			encodedRef := strings.TrimSuffix(r.URL.Path[strings.LastIndex(r.URL.Path, "/images/")+len("/images/"):], "/json")
			_, err := url.PathUnescape(encodedRef)
			if !assert.NoError(t, err) {
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(dockertypesimage.InspectResponse{
				ID:          imageID,
				RepoTags:    []string{imageRef},
				RepoDigests: []string{"ghcr.io/getarcaneapp/arcane@" + currentDigest},
			})) {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	require.NoError(t, db.WithContext(ctx).Create(&imageupdate.ImageUpdateRecord{
		ID:             imageID,
		Repository:     "ghcr.io/getarcaneapp/arcane",
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "latest",
		CurrentDigest:  &currentDigest,
		LatestDigest:   &latestDigest,
		CheckTime:      time.Now().UTC(),
	}).Error)

	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("unavailable")),
			Request:    req,
		}, nil
	})}
	dockerService := docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server))
	imageUpdateService := imageupdate.NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	svc := NewVersionService(httpClient, false, "1.2.3", "revision", nil, dockerService, imageUpdateService)

	info := svc.GetAppVersionInfo(ctx)

	require.NotNil(t, info)
	assert.True(t, info.IsSemverVersion)
	assert.False(t, info.UpdateAvailable)
	assert.Equal(t, latestDigest, info.NewestDigest)
	assert.Equal(t, currentDigest, info.CurrentDigest)
}

func TestVersionService_GetAppVersionInfoDisplaysSemverNextVersion(t *testing.T) {
	svc := NewVersionService(nil, true, "2.4.0-next.2", "2c3e44a10ddda540d7e19fc2a876c931fc33a426", nil, nil, nil)

	info := svc.GetAppVersionInfo(context.Background())

	require.NotNil(t, info)
	assert.Equal(t, "v2.4.0-next.2", info.DisplayVersion)
	assert.True(t, info.IsSemverVersion)
}

func TestVersionService_GetAppVersionInfoPreservesDevVersionInternal(t *testing.T) {
	svc := NewVersionService(nil, true, "dev", "unknown", nil, nil, nil)

	info := svc.GetAppVersionInfo(context.Background())

	require.NotNil(t, info)
	assert.Equal(t, "dev", info.CurrentVersion)
	assert.Equal(t, "dev", info.DisplayVersion)
	assert.False(t, info.IsSemverVersion)
}

// Test fixtures shared by this package's tests.

// setupImageUpdateTestDB creates an in-memory SQLite database for testing
func setupImageUpdateTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:image-update-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&imageupdate.ImageUpdateRecord{}, &event.Event{}, &project.Project{}))
	return &database.DB{DB: db}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// newTestDockerClientInternal builds a Docker client pointed at a fake Docker HTTP server.
func newTestDockerClientInternal(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()

	httpClient := server.Client()
	cli, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(httpClient),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = cli.Close()
	})

	return cli
}

func TestVersionService_IsNextBuildInternal(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		currentTag string
		want       bool
	}{
		{"next prerelease version", "2.8.0-next.65", "latest", true},
		{"stable version on next tag", "2.7.0", "next", true},
		{"stable version on latest tag", "2.7.0", "latest", false},
		{"stable version on version tag", "2.7.0", "v2.7.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewVersionService(nil, true, tt.version, "rev", nil, nil, nil)
			assert.Equal(t, tt.want, svc.isNextBuildInternal(tt.currentTag))
		})
	}
}

// newLabelRegistryTestServerInternal starts an in-memory OCI registry and
// returns its host as "localhost:PORT" so go-containerregistry uses plain HTTP.
func newLabelRegistryTestServerInternal(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(ggcrregistry.New(ggcrregistry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	return "localhost:" + serverURL.Port()
}

// newNextChannelDockerServerInternal fakes a Docker daemon whose Arcane
// container runs repo:tag with the given current repo digest.
func newNextChannelDockerServerInternal(t *testing.T, containerID, imageID, repo, tag, currentDigest string) *httptest.Server {
	t.Helper()
	imageRef := repo + ":" + tag
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{ID: containerID, State: container.StateRunning, Labels: map[string]string{labels.LabelArcane: "true"}},
			}))
		case strings.Contains(r.URL.Path, "/containers/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(container.InspectResponse{
				ID:    containerID,
				Image: imageID,
				Config: &container.Config{
					Image:  imageRef,
					Labels: map[string]string{labels.LabelArcane: "true"},
				},
			}))
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(dockertypesimage.InspectResponse{
				ID:          imageID,
				RepoTags:    []string{imageRef},
				RepoDigests: []string{repo + "@" + currentDigest},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestVersionService_GetAppVersionInfoNextChannelUsesImageLabelInternal(t *testing.T) {
	ctx := context.Background()
	db := setupImageUpdateTestDB(t)

	const (
		containerID = "arcane-container-1234567890"
		imageID     = "sha256:arcane-image"
	)
	registryHost := newLabelRegistryTestServerInternal(t)
	repo := registryHost + "/getarcaneapp/arcane"

	img, err := random.Image(64, 1)
	require.NoError(t, err)
	cfg, err := img.ConfigFile()
	require.NoError(t, err)
	cfg = cfg.DeepCopy()
	cfg.Config.Labels = map[string]string{"org.opencontainers.image.version": "v2.8.0-next.66"}
	img, err = mutate.ConfigFile(img, cfg)
	require.NoError(t, err)
	pushRef, err := name.ParseReference(repo + ":next")
	require.NoError(t, err)
	require.NoError(t, remote.Write(pushRef, img))
	imgDigest, err := img.Digest()
	require.NoError(t, err)

	currentDigest := digest.FromString("current-arcane").String()
	server := newNextChannelDockerServerInternal(t, containerID, imageID, repo, "next", currentDigest)

	require.NoError(t, db.WithContext(ctx).Create(&imageupdate.ImageUpdateRecord{
		ID:             imageID,
		Repository:     repo,
		Tag:            "next",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "next",
		CurrentDigest:  &currentDigest,
		LatestDigest:   func() *string { s := imgDigest.String(); return &s }(),
		CheckTime:      time.Now().UTC(),
	}).Error)

	githubHit := false
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		githubHit = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v2.7.0"}`)),
			Request:    req,
		}, nil
	})}
	dockerService := docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server))
	imageUpdateService := imageupdate.NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	registrySvc := registry.NewContainerRegistryService(nil, nil, nil)
	svc := NewVersionService(httpClient, false, "2.8.0-next.65", "revision", registrySvc, dockerService, imageUpdateService)

	info := svc.GetAppVersionInfo(ctx)

	require.NotNil(t, info)
	assert.Equal(t, "v2.8.0-next.66", info.NewestVersion)
	assert.True(t, info.UpdateAvailable)
	assert.Equal(t, imgDigest.String(), info.NewestDigest)
	assert.Empty(t, info.ReleaseNotes)
	assert.Empty(t, info.ReleaseURL)
	assert.False(t, githubHit, "next channel must not consult the GitHub stable release feed")
}

func TestVersionService_GetAppVersionInfoNextChannelLabelFailureFallsBackToDigestInternal(t *testing.T) {
	ctx := context.Background()
	db := setupImageUpdateTestDB(t)

	const (
		containerID = "arcane-container-1234567890"
		imageID     = "sha256:arcane-image"
	)
	// Registry with no image pushed: the label fetch fails.
	registryHost := newLabelRegistryTestServerInternal(t)
	repo := registryHost + "/getarcaneapp/arcane"

	currentDigest := digest.FromString("current-arcane").String()
	latestDigest := digest.FromString("latest-arcane").String()
	server := newNextChannelDockerServerInternal(t, containerID, imageID, repo, "next", currentDigest)

	require.NoError(t, db.WithContext(ctx).Create(&imageupdate.ImageUpdateRecord{
		ID:             imageID,
		Repository:     repo,
		Tag:            "next",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "next",
		CurrentDigest:  &currentDigest,
		LatestDigest:   &latestDigest,
		CheckTime:      time.Now().UTC(),
	}).Error)

	dockerService := docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server))
	imageUpdateService := imageupdate.NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	registrySvc := registry.NewContainerRegistryService(nil, nil, nil)
	svc := NewVersionService(nil, false, "2.8.0-next.65", "revision", registrySvc, dockerService, imageUpdateService)

	info := svc.GetAppVersionInfo(ctx)

	require.NotNil(t, info)
	assert.Empty(t, info.NewestVersion, "label failure must not fall back to a stable tag")
	assert.True(t, info.UpdateAvailable, "digest change alone flags the update")
	assert.Equal(t, latestDigest, info.NewestDigest)
}

func TestVersionService_GetAppVersionInfoSuppressesOlderStableForPrereleaseInternal(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v2.7.0","body":"notes","published_at":"2026-08-05T22:21:04Z"}`)),
			Request:    req,
		}, nil
	})}
	// An -rc. prerelease stays on the stable track; the downgrade guard must
	// still drop the older stable tag rather than showing a backwards arrow.
	svc := NewVersionService(httpClient, false, "2.8.0-rc.1", "revision", nil, nil, nil)

	info := svc.GetAppVersionInfo(context.Background())

	require.NotNil(t, info)
	assert.False(t, info.UpdateAvailable)
	assert.Empty(t, info.NewestVersion)
	assert.Empty(t, info.ReleaseNotes)
	assert.Empty(t, info.ReleaseURL)
	assert.Empty(t, info.ReleasedAt)
}
