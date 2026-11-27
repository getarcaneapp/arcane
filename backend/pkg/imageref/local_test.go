package imageref

import (
	"testing"

	mobyImage "github.com/moby/moby/api/types/image"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestIsLocalImage(t *testing.T) {
	const repoDigest = "alpine@sha256:51b67269f354137895d43f3b3d810bfacd3945438e94dc5ac55fdac340352f48"
	imported := &ocispec.Descriptor{Annotations: map[string]string{imageNameAnnotation: "docker.io/library/app:dev"}}

	tests := []struct {
		img  mobyImage.InspectResponse
		name string
		want bool
	}{
		{
			name: "pulled image",
			img: mobyImage.InspectResponse{
				RepoDigests: []string{repoDigest},
				Identity:    &mobyImage.Identity{Pull: []mobyImage.PullIdentity{{Repository: "docker.io/library/alpine"}}},
			},
		},
		{
			name: "built then pushed image",
			img: mobyImage.InspectResponse{
				RepoDigests: []string{repoDigest},
				Descriptor:  imported,
				Identity: &mobyImage.Identity{
					Pull:  []mobyImage.PullIdentity{{Repository: "docker.io/library/app"}},
					Build: []mobyImage.BuildIdentity{{Ref: "build-ref"}},
				},
			},
		},
		{
			name: "image built on this daemon",
			img: mobyImage.InspectResponse{
				RepoDigests: []string{repoDigest},
				Identity:    &mobyImage.Identity{Build: []mobyImage.BuildIdentity{{Ref: "build-ref"}}},
			},
			want: true,
		},
		{
			name: "loaded or BuildKit-exported image",
			img:  mobyImage.InspectResponse{RepoDigests: []string{repoDigest}, Descriptor: imported},
			want: true,
		},
		{
			name: "image pulled before Docker recorded identity",
			img:  mobyImage.InspectResponse{RepoDigests: []string{repoDigest}, Descriptor: &ocispec.Descriptor{}},
		},
		{
			name: "classic store image without repo digests",
			img:  mobyImage.InspectResponse{RepoDigests: []string{"<none>@<none>"}},
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
