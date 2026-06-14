package projects

import (
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
)

func TestMergeBuildTags(t *testing.T) {
	tags := MergeBuildTags("example/app:latest", []string{"example/app:sha", "example/app:latest", " "})
	assert.Equal(t, []string{"example/app:latest", "example/app:sha"}, tags)
}

func TestBuildPlatformsFromCompose(t *testing.T) {
	t.Run("uses service platform when build platforms missing", func(t *testing.T) {
		svc := composetypes.ServiceConfig{
			Platform: "linux/amd64",
			Build: &composetypes.BuildConfig{
				Context: ".",
			},
		}

		platforms := BuildPlatformsFromCompose(svc)
		assert.Equal(t, []string{"linux/amd64"}, platforms)
	})

	t.Run("keeps explicit build platforms", func(t *testing.T) {
		svc := composetypes.ServiceConfig{
			Platform: "linux/amd64",
			Build: &composetypes.BuildConfig{
				Context:   ".",
				Platforms: []string{"linux/arm64"},
			},
		}

		platforms := BuildPlatformsFromCompose(svc)
		assert.Equal(t, []string{"linux/arm64"}, platforms)
	})
}
