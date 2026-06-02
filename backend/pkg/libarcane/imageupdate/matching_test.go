package imageupdate

import (
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"

	dockerutil "github.com/getarcaneapp/arcane/backend/pkg/dockerutil"
)

func TestAppendImageUpdateRecordIDToOldIDs(t *testing.T) {
	tests := []struct {
		name     string
		oldIDs   []string
		recordID string
		want     []string
	}{
		{
			name:     "appends image update record id when it is an image id",
			oldIDs:   nil,
			recordID: "sha256:old-image",
			want:     []string{"sha256:old-image"},
		},
		{
			name:     "does not duplicate existing image id",
			oldIDs:   []string{"sha256:old-image"},
			recordID: "sha256:old-image",
			want:     []string{"sha256:old-image"},
		},
		{
			name:     "ignores synthetic ref record ids",
			oldIDs:   []string{"sha256:old-image"},
			recordID: "ref::registry.example.com/team/app@latest",
			want:     []string{"sha256:old-image"},
		},
		{
			name:     "ignores empty record ids",
			oldIDs:   []string{"sha256:old-image"},
			recordID: " ",
			want:     []string{"sha256:old-image"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AppendImageUpdateRecordIDToOldIDs(tt.oldIDs, tt.recordID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsImageIDLikeReference(t *testing.T) {
	assert.True(t, IsImageIDLikeReference("sha256:abcdef"))
	assert.True(t, IsImageIDLikeReference("SHA256:ABCDEF"))
	assert.False(t, IsImageIDLikeReference("nginx:latest"))
	assert.False(t, IsImageIDLikeReference("docker.io/library/nginx:latest"))
}

func TestShouldInspectUnmatchedContainerForImageMatch(t *testing.T) {
	composeLabels := map[string]string{
		dockerutil.ComposeProjectLabelKey: "myproj",
		dockerutil.ComposeServiceLabelKey: "web",
	}

	// Empty or image-ID-like summary values always need an inspect to recover a tag.
	assert.True(t, ShouldInspectUnmatchedContainerForImageMatch(container.Summary{Image: ""}))
	assert.True(t, ShouldInspectUnmatchedContainerForImageMatch(container.Summary{Image: "sha256:abcdef"}))

	// A plain named reference is already matchable: no inspect even with compose labels.
	assert.False(t, ShouldInspectUnmatchedContainerForImageMatch(container.Summary{Image: "nginx:1.25", Labels: composeLabels}))

	// A digest-pinned compose container loses its tag, so fall back to an inspect.
	digestRef := "nginx@sha256:" + strings.Repeat("a", 64)
	assert.True(t, ShouldInspectUnmatchedContainerForImageMatch(container.Summary{Image: digestRef, Labels: composeLabels}))

	// A digest-pinned container without compose labels has no tag to recover.
	assert.False(t, ShouldInspectUnmatchedContainerForImageMatch(container.Summary{Image: digestRef}))
}

func TestResolveContainerImageMatch(t *testing.T) {
	updatedNorm := map[string]string{
		NormalizeImageUpdateRef("nginx:latest"): "nginx:latest",
	}
	oldIDToNewRef := map[string]string{
		"sha256:img1": "redis:7",
	}

	tests := []struct {
		name        string
		container   container.Summary
		inspect     *container.InspectResponse
		updatedNorm map[string]string
		wantRef     string
		wantMatchID string
	}{
		{
			name: "match by image id",
			container: container.Summary{
				ImageID: "sha256:img1",
				Image:   "some/other:tag",
			},
			wantRef:     "redis:7",
			wantMatchID: "sha256:img1",
		},
		{
			name: "match by normalized image tag from summary",
			container: container.Summary{
				ImageID: "sha256:unknown",
				Image:   "docker.io/library/nginx:latest",
			},
			wantRef:     "nginx:latest",
			wantMatchID: NormalizeImageUpdateRef("nginx:latest"),
		},
		{
			name: "match by inspected config image when summary is image id-like",
			container: container.Summary{
				ImageID: "sha256:unknown",
				Image:   "sha256:abcdef",
			},
			inspect: &container.InspectResponse{
				Image: "sha256:unknown",
				Config: &container.Config{
					Image: "docker.io/library/nginx:latest",
				},
			},
			wantRef:     "nginx:latest",
			wantMatchID: NormalizeImageUpdateRef("nginx:latest"),
		},
		{
			name: "image id-like summary value cannot be tag matched",
			container: container.Summary{
				ImageID: "sha256:unknown",
				Image:   "sha256:abcdef",
			},
			wantRef:     "",
			wantMatchID: "",
		},
		{
			name: "invalid image reference does not match empty normalized key",
			container: container.Summary{
				ImageID: "sha256:unknown",
				Image:   "Bad/Image:latest",
			},
			updatedNorm: map[string]string{"": "wrong:latest"},
			wantRef:     "",
			wantMatchID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localUpdatedNorm := updatedNorm
			if tt.updatedNorm != nil {
				localUpdatedNorm = tt.updatedNorm
			}
			gotRef, gotMatch := ResolveContainerImageMatch(tt.container, tt.inspect, oldIDToNewRef, localUpdatedNorm)
			assert.Equal(t, tt.wantRef, gotRef)
			assert.Equal(t, tt.wantMatchID, gotMatch)
		})
	}
}
