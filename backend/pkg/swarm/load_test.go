package swarm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

func TestLoadComposeProjectMergesOverrideContent(t *testing.T) {
	base := "services:\n  app:\n    image: nginx:alpine\n    environment:\n      FROM_BASE: \"1\"\n"
	override := "services:\n  app:\n    image: busybox:latest\n    environment:\n      FROM_OVERRIDE: \"1\"\n"

	project, err := projects.LoadComposeProjectFromContent(t.Context(), types.ComposeContentOptions{
		ProjectName:     "stack",
		ComposeContent:  base,
		OverrideContent: override,
	})
	require.NoError(t, err)

	app := project.Services["app"]
	require.Equal(t, "busybox:latest", app.Image)
	require.Contains(t, app.Environment, "FROM_BASE")
	require.Contains(t, app.Environment, "FROM_OVERRIDE")
}
