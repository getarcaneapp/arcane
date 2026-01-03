package arcaneupdater

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/getarcaneapp/arcane/backend/internal/utils/registry"
)

// DigestChecker provides methods to check if an image needs updating by comparing digests
type DigestChecker struct {
	dcli           *client.Client
	registryClient *registry.Client
}

// NewDigestChecker creates a new DigestChecker
func NewDigestChecker(dcli *client.Client, registryClient *registry.Client) *DigestChecker {
	return &DigestChecker{
		dcli:           dcli,
		registryClient: registryClient,
	}
}

// CheckResult contains the result of a digest check
type CheckResult struct {
	NeedsUpdate   bool
	LocalDigest   string
	RemoteDigest  string
	Error         error
	CheckedViaAPI bool // True if we checked via registry API, false if we had to pull
}

// CheckImageNeedsUpdate checks if an image has a newer version available without pulling
// Returns true if the remote digest differs from the local digest
func (c *DigestChecker) CheckImageNeedsUpdate(ctx context.Context, imageRef string, authToken string) CheckResult {
	result := CheckResult{}

	// Parse image reference
	registryHost, repository, tag := parseImageRef(imageRef)

	slog.DebugContext(ctx, "CheckImageNeedsUpdate: checking image",
		"imageRef", imageRef,
		"registry", registryHost,
		"repository", repository,
		"tag", tag)

	// Get local digest
	localDigest, err := c.getLocalDigest(ctx, imageRef)
	if err != nil {
		slog.DebugContext(ctx, "CheckImageNeedsUpdate: failed to get local digest",
			"imageRef", imageRef,
			"error", err)
		// Image not present locally - definitely needs update
		result.NeedsUpdate = true
		result.Error = err
		return result
	}
	result.LocalDigest = localDigest

	// Get remote digest via HEAD request
	remoteDigest, err := c.registryClient.GetLatestDigest(ctx, registryHost, repository, tag, authToken)
	if err != nil {
		slog.DebugContext(ctx, "CheckImageNeedsUpdate: failed to get remote digest via HEAD",
			"imageRef", imageRef,
			"error", err)
		// Can't determine remotely - caller should fall back to pull
		result.Error = err
		return result
	}

	result.RemoteDigest = remoteDigest
	result.CheckedViaAPI = true
	result.NeedsUpdate = localDigest != remoteDigest

	slog.DebugContext(ctx, "CheckImageNeedsUpdate: digest comparison complete",
		"imageRef", imageRef,
		"localDigest", localDigest,
		"remoteDigest", remoteDigest,
		"needsUpdate", result.NeedsUpdate)

	return result
}

// getLocalDigest retrieves the digest of a locally stored image
func (c *DigestChecker) getLocalDigest(ctx context.Context, imageRef string) (string, error) {
	inspect, err := c.dcli.ImageInspect(ctx, imageRef)
	if err != nil {
		return "", fmt.Errorf("image not found locally: %w", err)
	}

	// Try to get digest from RepoDigests
	for _, rd := range inspect.RepoDigests {
		if strings.Contains(rd, "@sha256:") {
			parts := strings.Split(rd, "@")
			if len(parts) == 2 {
				return parts[1], nil
			}
		}
	}

	// Fall back to image ID (which is a content-addressed hash)
	if inspect.ID != "" {
		return inspect.ID, nil
	}

	return "", fmt.Errorf("no digest available for image")
}

// CompareWithPulled compares the current container's image with a freshly pulled image
// This is the fallback when HEAD request doesn't work
func (c *DigestChecker) CompareWithPulled(ctx context.Context, containerImageID string, newImageRef string) (bool, error) {
	// Get the new image info after pull
	newInspect, err := c.dcli.ImageInspect(ctx, newImageRef)
	if err != nil {
		return false, fmt.Errorf("failed to inspect new image: %w", err)
	}

	// Compare image IDs
	return containerImageID != newInspect.ID, nil
}

// GetImageIDsForRef returns the image IDs associated with a reference
func (c *DigestChecker) GetImageIDsForRef(ctx context.Context, ref string) ([]string, error) {
	// First try direct inspect
	inspect, err := c.dcli.ImageInspect(ctx, ref)
	if err == nil && inspect.ID != "" {
		return []string{inspect.ID}, nil
	}

	// Fall back to listing and filtering
	images, err := c.dcli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return nil, err
	}

	normalizedRef := normalizeRef(ref)
	var ids []string
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if normalizeRef(tag) == normalizedRef {
				ids = append(ids, img.ID)
				break
			}
		}
	}

	return ids, nil
}

// parseImageRef splits an image reference into registry, repository, and tag
func parseImageRef(ref string) (registry, repository, tag string) {
	// Strip digest if present
	if i := strings.Index(ref, "@"); i != -1 {
		ref = ref[:i]
	}

	// Default tag
	tag = "latest"
	if i := strings.LastIndex(ref, ":"); i != -1 && strings.LastIndex(ref, "/") < i {
		tag = ref[i+1:]
		ref = ref[:i]
	}

	// Parse registry and repository
	parts := strings.Split(ref, "/")
	switch {
	case len(parts) > 0 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") || parts[0] == "localhost"):
		registry = parts[0]
		repository = strings.Join(parts[1:], "/")
	default:
		registry = "docker.io"
		repository = ref
		// Docker Hub official images are in library/
		if !strings.Contains(repository, "/") {
			repository = "library/" + repository
		}
	}

	return registry, repository, tag
}

// normalizeRef normalizes an image reference for comparison
func normalizeRef(ref string) string {
	// Strip digest
	if i := strings.Index(ref, "@"); i != -1 {
		ref = ref[:i]
	}

	// Parse and reconstruct
	reg, repo, tag := parseImageRef(ref)

	// Normalize docker.io variants
	switch reg {
	case "index.docker.io", "registry-1.docker.io":
		reg = "docker.io"
	}

	return strings.ToLower(reg + "/" + repo + ":" + tag)
}
