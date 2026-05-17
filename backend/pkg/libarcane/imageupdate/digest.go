package imageupdate

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/moby/moby/client"
	digest "github.com/opencontainers/go-digest"
)

type remoteDigestResolver interface {
	GetImageDigest(ctx context.Context, imageRef string) (string, error)
}

// DigestChecker provides methods to check if an image needs updating by comparing digests
type DigestChecker struct {
	dcli           *client.Client
	digestResolver remoteDigestResolver
}

// NewDigestChecker creates a new DigestChecker
func NewDigestChecker(dcli *client.Client, digestResolver remoteDigestResolver) *DigestChecker {
	return &DigestChecker{
		dcli:           dcli,
		digestResolver: digestResolver,
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

func NormalizeDigest(value string) (string, error) {
	parsed, err := digest.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("invalid OCI digest %q: %w", value, err)
	}

	return parsed.String(), nil
}

func DigestFromReferenceSuffix(ref string) (string, bool) {
	_, digestValue, ok := strings.Cut(strings.TrimSpace(ref), "@")
	if !ok {
		return "", false
	}

	normalized, err := NormalizeDigest(digestValue)
	if err != nil {
		return "", false
	}

	return normalized, true
}

// CheckImageNeedsUpdate checks if an image has a newer version available without pulling
// Returns true if the remote digest differs from the local digest
func (c *DigestChecker) CheckImageNeedsUpdate(ctx context.Context, imageRef string) CheckResult {
	result := CheckResult{}

	slog.DebugContext(ctx, "CheckImageNeedsUpdate: checking image",
		"imageRef", imageRef,
		"normalizedRef", normalizedReferenceStringInternal(imageRef))

	// Get local digest
	localDigest, err := c.getLocalDigestInternal(ctx, imageRef)
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

	if c.digestResolver == nil {
		result.Error = fmt.Errorf("remote digest resolver unavailable")
		return result
	}

	// Get remote digest via the Docker daemon-backed registry path.
	remoteDigest, err := c.digestResolver.GetImageDigest(ctx, imageRef)
	if err != nil {
		slog.DebugContext(ctx, "CheckImageNeedsUpdate: failed to get remote digest",
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

// getLocalDigestInternal retrieves the digest of a locally stored image
func (c *DigestChecker) getLocalDigestInternal(ctx context.Context, imageRef string) (string, error) {
	inspect, err := c.dcli.ImageInspect(ctx, imageRef)
	if err != nil {
		return "", fmt.Errorf("image not found locally: %w", err)
	}

	// Try to get digest from RepoDigests
	for _, rd := range inspect.RepoDigests {
		if normalized, ok := DigestFromReferenceSuffix(rd); ok {
			return normalized, nil
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
	imageList, err := c.dcli.ImageList(ctx, client.ImageListOptions{})
	if err != nil {
		return nil, err
	}
	images := imageList.Items

	normalizedRef := normalizedReferenceStringInternal(ref)
	var ids []string
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if normalizedReferenceStringInternal(tag) == normalizedRef {
				ids = append(ids, img.ID)
				break
			}
		}
	}

	return ids, nil
}

func normalizedReferenceStringInternal(imageRef string) string {
	parts, err := NormalizeReference(imageRef)
	if err != nil {
		return ""
	}
	return parts.NormalizedRef
}
