package imageref

import (
	"cmp"
	"context"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"kit.getarcane.app"
	"kit.getarcane.app/docker/compat"
	"kit.getarcane.app/updater/refs"
)

// ConfiguredImage returns the image a container was created from. Docker lists
// a container whose image lost its tag by image ID, so the configured image is
// inspected only then; the listed name is the fallback.
func ConfiguredImage(ctx context.Context, dockerClient *client.Client, summary container.Summary) string {
	if !refs.IsImageIDLikeReference(summary.Image) {
		return summary.Image
	}
	result, err := compat.ContainerInspectWithCompatibility(ctx, dockerClient, summary.ID, client.ContainerInspectOptions{})
	if err != nil {
		return summary.Image
	}
	return cmp.Or(kit.FromPtr(result.Container.Config).Image, summary.Image)
}
