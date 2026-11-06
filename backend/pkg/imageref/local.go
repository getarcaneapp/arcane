package imageref

import (
	"slices"

	"github.com/moby/moby/api/types/image"
)

// imageNameAnnotation is set on the descriptor of an image imported from an
// image archive or exported by BuildKit (docker load, --load, Copacetic). The
// containerd image store never sets it on a pulled image.
const imageNameAnnotation = "io.containerd.image.name"

// IsLocalImage reports whether an image exists only on this daemon, so no
// registry holds it to check for updates or to patch from.
//
// The containerd image store gives every image a repo digest, so repo digests
// alone cannot tell a pull from a build. Docker 29+ records where an image came
// from in Identity; images without it fall back to the descriptor annotation
// an import leaves, then to the repo-digest rule of the classic image store.
func IsLocalImage(img image.InspectResponse) bool {
	switch {
	case img.Identity != nil && len(img.Identity.Pull) > 0:
		return false
	case img.Identity != nil && len(img.Identity.Build) > 0:
		return true
	case img.Descriptor != nil && img.Descriptor.Annotations[imageNameAnnotation] != "":
		return true
	default:
		return !slices.ContainsFunc(img.RepoDigests, func(repoDigest string) bool { return repoDigest != "<none>@<none>" })
	}
}
