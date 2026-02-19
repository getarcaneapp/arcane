package services

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/stretchr/testify/assert"
)

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
