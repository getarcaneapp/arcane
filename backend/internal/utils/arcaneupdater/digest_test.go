package arcaneupdater

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseImageRef(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
		wantHost string
		wantRepo string
		wantTag  string
	}{
		{
			name:     "docker hub official image",
			imageRef: "nginx:latest",
			wantHost: "docker.io",
			wantRepo: "library/nginx",
			wantTag:  "latest",
		},
		{
			name:     "custom registry image",
			imageRef: "ghcr.io/getarcaneapp/arcane:v1.2.3",
			wantHost: "ghcr.io",
			wantRepo: "getarcaneapp/arcane",
			wantTag:  "v1.2.3",
		},
		{
			name:     "digest reference defaults to latest tag",
			imageRef: "docker.io/library/redis@sha256:abcdef",
			wantHost: "docker.io",
			wantRepo: "library/redis",
			wantTag:  "latest",
		},
		{
			name:     "docker registry variant is normalized",
			imageRef: "registry-1.docker.io/library/busybox:1.36",
			wantHost: "docker.io",
			wantRepo: "library/busybox",
			wantTag:  "1.36",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, repo, tag := parseImageRef(tt.imageRef)
			assert.Equal(t, tt.wantHost, host)
			assert.Equal(t, tt.wantRepo, repo)
			assert.Equal(t, tt.wantTag, tag)
		})
	}
}

func TestNormalizeRef(t *testing.T) {
	assert.Equal(t, "docker.io/library/nginx:latest", normalizeRef("nginx"))
	assert.Equal(t, "docker.io/library/nginx:latest", normalizeRef("index.docker.io/library/nginx:latest"))
	assert.Equal(t, "ghcr.io/getarcaneapp/arcane:v1", normalizeRef("ghcr.io/getarcaneapp/arcane:v1"))
}
