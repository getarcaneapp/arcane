package imageref

import (
	"context"
	"fmt"

	"github.com/moby/moby/client"

	"kit.getarcane.app/updater/refs"
)

// PullAsTag runs pull for the immutable pulled reference, tags it as selected,
// and verifies the tag resolves to the pulled image. Both must name the same
// repository. The digest name is kept on purpose: removing it deletes the image
// on the containerd image store.
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
	return nil
}
