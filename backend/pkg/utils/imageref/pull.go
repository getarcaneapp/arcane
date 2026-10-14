package imageref

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/distribution/reference"
	"github.com/moby/moby/client"
	"go.getarcane.app/updater/refs"
)

// PullAsTag runs pull for the immutable pulled reference, tags it as selected,
// and verifies the tag resolves to the pulled image. Both must name the same
// repository. A digest name the pull added to RepoTags (containerd image store)
// is removed so only the selected tag remains.
func PullAsTag(ctx context.Context, dockerClient *client.Client, pulled, selected string, pull func(context.Context) error) error {
	pulledRef, err := refs.NormalizeReference(pulled)
	if err != nil {
		return err
	}
	selectedRef, err := refs.NormalizeReference(selected)
	if err != nil {
		return err
	}
	if pulledRef.RegistryHost != selectedRef.RegistryHost || pulledRef.Repository != selectedRef.Repository {
		return fmt.Errorf("pulled image %s does not belong to repository of %s", pulled, selected)
	}

	hasPulledName := func(repoTags []string) bool {
		pulledName, parseErr := reference.ParseNormalizedNamed(pulled)
		return parseErr == nil && slices.ContainsFunc(repoTags, func(tag string) bool {
			named, tagErr := reference.ParseNormalizedNamed(tag)
			return tagErr == nil && named.String() == pulledName.String()
		})
	}
	before, inspectErr := dockerClient.ImageInspect(ctx, pulled)
	hadPulledName := inspectErr == nil && hasPulledName(before.RepoTags)

	if pullErr := pull(ctx); pullErr != nil {
		return pullErr
	}
	if _, tagErr := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: pulled, Target: selected}); tagErr != nil {
		return tagErr
	}
	pulledInspect, err := dockerClient.ImageInspect(ctx, pulled)
	if err != nil {
		return fmt.Errorf("inspect pulled image: %w", err)
	}
	selectedInspect, err := dockerClient.ImageInspect(ctx, selected)
	if err != nil {
		return fmt.Errorf("inspect tagged image: %w", err)
	}
	if pulledInspect.ID != selectedInspect.ID {
		return fmt.Errorf("%s resolves to %s after tagging, expected %s", selected, selectedInspect.ID, pulledInspect.ID)
	}

	// The tag already holds the image, so a leftover digest name is only cosmetic.
	if !hadPulledName && hasPulledName(selectedInspect.RepoTags) {
		if _, removeErr := dockerClient.ImageRemove(ctx, pulled, client.ImageRemoveOptions{}); removeErr != nil {
			slog.WarnContext(ctx, "failed to remove pulled digest reference", "image", pulled, "error", removeErr)
		}
	}
	return nil
}
