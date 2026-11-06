package imageref

import (
	"testing"

	"github.com/moby/moby/api/types/image"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestIsLocalImage(t *testing.T) {
	const repoDigest = "alpine@sha256:51b67269f354137895d43f3b3d810bfacd3945438e94dc5ac55fdac340352f48"
	imported := &ocispec.Descriptor{Annotations: map[string]string{imageNameAnnotation: "docker.io/library/app:dev"}}

	tests := []struct {
		img  image.InspectResponse
		name string
		want bool
	}{
		{
			name: "pulled image",
			img: image.InspectResponse{
				RepoDigests: []string{repoDigest},
				Identity:    &image.Identity{Pull: []image.PullIdentity{{Repository: "docker.io/library/alpine"}}},
			},
		},
		{
			name: "built then pushed image",
			img: image.InspectResponse{
				RepoDigests: []string{repoDigest},
				Descriptor:  imported,
				Identity: &image.Identity{
					Pull:  []image.PullIdentity{{Repository: "docker.io/library/app"}},
					Build: []image.BuildIdentity{{Ref: "build-ref"}},
				},
			},
		},
		{
			name: "image built on this daemon",
			img: image.InspectResponse{
				RepoDigests: []string{repoDigest},
				Identity:    &image.Identity{Build: []image.BuildIdentity{{Ref: "build-ref"}}},
			},
			want: true,
		},
		{
			name: "loaded or BuildKit-exported image",
			img:  image.InspectResponse{RepoDigests: []string{repoDigest}, Descriptor: imported},
			want: true,
		},
		{
			name: "image pulled before Docker recorded identity",
			img:  image.InspectResponse{RepoDigests: []string{repoDigest}, Descriptor: &ocispec.Descriptor{}},
		},
		{
			name: "classic store image without repo digests",
			img:  image.InspectResponse{RepoDigests: []string{"<none>@<none>"}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLocalImage(tt.img); got != tt.want {
				t.Fatalf("IsLocalImage() = %v, want %v", got, tt.want)
			}
		})
	}
}
