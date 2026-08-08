package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
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

	require.NoError(t, db.WithContext(ctx).Create(&models.ImageUpdateRecord{
		ID:             imageID,
		Repository:     "ghcr.io/getarcaneapp/arcane",
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
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
	require.NoError(t, db.AutoMigrate(&models.ImageUpdateRecord{}, &models.Event{}, &models.Project{}))
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
