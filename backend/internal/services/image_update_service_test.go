package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ref "go.podman.io/image/v5/docker/reference"
)

// TestParseImageReference tests the parseImageReference function with various image formats
// This is used for digest-based update checking
func TestImageUpdateService_ParseImageReference(t *testing.T) {
	tests := []struct {
		name           string
		imageRef       string
		wantRegistry   string
		wantRepository string
		wantTag        string
	}{
		{
			name:           "Docker Hub official image with tag",
			imageRef:       "redis:latest",
			wantRegistry:   "docker.io",
			wantRepository: "library/redis",
			wantTag:        "latest",
		},
		{
			name:           "Docker Hub official image without tag",
			imageRef:       "nginx",
			wantRegistry:   "docker.io",
			wantRepository: "library/nginx",
			wantTag:        "latest",
		},
		{
			name:           "Docker Hub user image",
			imageRef:       "traefik/traefik:v2.10",
			wantRegistry:   "docker.io",
			wantRepository: "traefik/traefik",
			wantTag:        "v2.10",
		},
		{
			name:           "Custom registry with port",
			imageRef:       "localhost:5000/myapp:v1.0",
			wantRegistry:   "localhost:5000",
			wantRepository: "myapp",
			wantTag:        "v1.0",
		},
		{
			name:           "Custom registry with subdomain",
			imageRef:       "docker.getoutline.com/outlinewiki/outline:latest",
			wantRegistry:   "docker.getoutline.com",
			wantRepository: "outlinewiki/outline",
			wantTag:        "latest",
		},
		{
			name:           "GCR image",
			imageRef:       "gcr.io/google-containers/nginx:1.21",
			wantRegistry:   "gcr.io",
			wantRepository: "google-containers/nginx",
			wantTag:        "1.21",
		},
		{
			name:           "GHCR image",
			imageRef:       "ghcr.io/owner/repo:main",
			wantRegistry:   "ghcr.io",
			wantRepository: "owner/repo",
			wantTag:        "main",
		},
		{
			name:           "Multi-path repository",
			imageRef:       "registry.example.com/team/project/app:v2.0.0",
			wantRegistry:   "registry.example.com",
			wantRepository: "team/project/app",
			wantTag:        "v2.0.0",
		},
		{
			name:           "Image with digest",
			imageRef:       "alpine@sha256:1234567890abcdef",
			wantRegistry:   "docker.io",
			wantRepository: "library/alpine",
			wantTag:        "latest",
		},
		{
			name:           "Custom registry image with digest",
			imageRef:       "registry.io/app/service@sha256:abcdef123456",
			wantRegistry:   "registry.io",
			wantRepository: "app/service",
			wantTag:        "latest",
		},
	}

	svc := &ImageUpdateService{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := svc.parseImageReference(tt.imageRef)
			require.NotNil(t, parts, "parseImageReference returned nil")

			assert.Equal(t, tt.wantRegistry, parts.Registry, "registry mismatch")
			assert.Equal(t, tt.wantRepository, parts.Repository, "repository mismatch")
			assert.Equal(t, tt.wantTag, parts.Tag, "tag mismatch")
		})
	}
}

// TestParseImageReference_Fallback tests edge cases that might trigger fallback parsing
func TestImageUpdateService_ParseImageReference_Fallback(t *testing.T) {
	svc := &ImageUpdateService{}

	// Test malformed references that should still be parsed by fallback
	tests := []struct {
		name     string
		imageRef string
		wantNil  bool
	}{
		{
			name:     "Empty string",
			imageRef: "",
			wantNil:  false, // Fallback should handle it
		},
		{
			name:     "Valid reference",
			imageRef: "nginx:latest",
			wantNil:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := svc.parseImageReference(tt.imageRef)
			if tt.wantNil {
				assert.Nil(t, parts)
			} else {
				assert.NotNil(t, parts)
			}
		})
	}
}

// TestNormalizeRepository tests repository normalization
func TestImageUpdateService_NormalizeRepository(t *testing.T) {
	tests := []struct {
		name       string
		regHost    string
		repo       string
		wantNormal string
	}{
		{
			name:       "Docker Hub single name adds library",
			regHost:    "docker.io",
			repo:       "redis",
			wantNormal: "library/redis",
		},
		{
			name:       "Docker Hub with slash unchanged",
			regHost:    "docker.io",
			repo:       "traefik/traefik",
			wantNormal: "traefik/traefik",
		},
		{
			name:       "Custom registry unchanged",
			regHost:    "gcr.io",
			repo:       "project/app",
			wantNormal: "project/app",
		},
		{
			name:       "Custom registry single name unchanged",
			regHost:    "gcr.io",
			repo:       "nginx",
			wantNormal: "nginx",
		},
	}

	svc := &ImageUpdateService{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := svc.normalizeRepository(tt.regHost, tt.repo)
			assert.Equal(t, tt.wantNormal, result, "repository normalization mismatch")
		})
	}
}

// TestGetLocalImageDigestWithAll_ExtractsAllDigests tests that all digests are collected
func TestImageUpdateService_GetLocalImageDigestWithAll_Logic(t *testing.T) {
	// This is a unit test for the digest extraction logic
	// In a real scenario, you'd need to mock Docker client
	t.Run("Multiple digests in RepoDigests", func(t *testing.T) {
		// This test demonstrates the expected behavior
		// In practice, you'd use a mock Docker client
		repoDigests := []string{
			"docker.io/library/redis@sha256:abc123",
			"redis@sha256:def456",
		}

		var allDigests []string
		for _, repoDigest := range repoDigests {
			parts := splitRepoDigest(repoDigest)
			if parts != nil {
				allDigests = append(allDigests, parts.digest)
			}
		}

		assert.Len(t, allDigests, 2)
		assert.Contains(t, allDigests, "sha256:abc123")
		assert.Contains(t, allDigests, "sha256:def456")
	})
}

// Helper function to test digest splitting
type repoDigestParts struct {
	repo   string
	digest string
}

func splitRepoDigest(repoDigest string) *repoDigestParts {
	parts := splitString(repoDigest, "@")
	if len(parts) == 2 {
		return &repoDigestParts{
			repo:   parts[0],
			digest: parts[1],
		}
	}
	return nil
}

func splitString(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

// TestDockerReferenceCompatibility ensures our parser is compatible with Docker's reference package
func TestImageUpdateService_DockerReferenceCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
	}{
		{"Docker Hub official", "nginx:latest"},
		{"Docker Hub user", "traefik/traefik:v2.0"},
		{"Custom registry", "gcr.io/project/app:v1"},
		{"With port", "localhost:5000/app:tag"},
		{"Multi-path", "registry.io/team/project/app:latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that official parser can handle it
			named, err := ref.ParseNormalizedNamed(tt.imageRef)
			require.NoError(t, err, "official parser failed")

			// Test our parser
			svc := &ImageUpdateService{}
			parts := svc.parseImageReference(tt.imageRef)
			require.NotNil(t, parts, "our parser returned nil")

			// Verify they produce the same results
			assert.Equal(t, ref.Domain(named), parts.Registry)
			assert.Equal(t, ref.Path(named), parts.Repository)
		})
	}
}
