package swarm

import (
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
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
