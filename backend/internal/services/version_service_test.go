package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	libupdater "github.com/getarcaneapp/updater/pkg/labels"
	"github.com/moby/moby/api/types/container"
	dockertypesimage "github.com/moby/moby/api/types/image"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{
					ID:    containerID,
					State: container.StateRunning,
					Labels: map[string]string{
						libupdater.LabelArcane: "true",
					},
				},
			}))
		case strings.Contains(r.URL.Path, "/containers/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(container.InspectResponse{
				ID:    containerID,
				Image: imageID,
				Config: &container.Config{
					Image: imageRef,
					Labels: map[string]string{
						libupdater.LabelArcane: "true",
					},
				},
			}))
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			encodedRef := strings.TrimSuffix(r.URL.Path[strings.LastIndex(r.URL.Path, "/images/")+len("/images/"):], "/json")
			_, err := url.PathUnescape(encodedRef)
			require.NoError(t, err)

			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(dockertypesimage.InspectResponse{
				ID:          imageID,
				RepoTags:    []string{imageRef},
				RepoDigests: []string{"ghcr.io/getarcaneapp/arcane@" + currentDigest},
			}))
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
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	svc := NewVersionService(httpClient, false, "1.2.3", "revision", nil, dockerService, imageUpdateService)

	info := svc.GetAppVersionInfo(ctx)

	require.NotNil(t, info)
	assert.True(t, info.IsSemverVersion)
	assert.False(t, info.UpdateAvailable)
	assert.Equal(t, latestDigest, info.NewestDigest)
	assert.Equal(t, currentDigest, info.CurrentDigest)
}
