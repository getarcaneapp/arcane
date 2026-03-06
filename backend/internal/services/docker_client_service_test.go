package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDockerClient_UsesConfiguredHostAndPinsServerAPIVersion(t *testing.T) {
	t.Setenv("DOCKER_API_VERSION", "1.54")
	t.Setenv("DOCKER_HOST", "tcp://docker-from-env:2375")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_ping" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Api-Version", "1.41")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	cli, err := newDockerClientInternal(context.Background(), server.URL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = cli.Close()
	})

	assert.Equal(t, server.URL, cli.DaemonHost())
	assert.Equal(t, "1.41", cli.ClientVersion())
}

func TestCountImageUsage_UsesContainerImageIDs(t *testing.T) {
	images := []image.Summary{
		{ID: "sha256:image-a", Containers: -1},
		{ID: "sha256:image-b", Containers: 0},
		{ID: "sha256:image-c", Containers: 99},
	}

	containers := []container.Summary{
		{ImageID: "sha256:image-a"},
		{ImageID: "sha256:image-c"},
		{ImageID: "sha256:image-a"}, // duplicate container ref should not affect counts
		{ImageID: ""},
	}

	inuse, unused, total := countImageUsageInternal(images, containers)

	assert.Equal(t, 2, inuse)
	assert.Equal(t, 1, unused)
	assert.Equal(t, 3, total)
}

func TestCountImageUsage_NoImages(t *testing.T) {
	inuse, unused, total := countImageUsageInternal(nil, []container.Summary{{ImageID: "sha256:image-a"}})

	assert.Equal(t, 0, inuse)
	assert.Equal(t, 0, unused)
	assert.Equal(t, 0, total)
}
