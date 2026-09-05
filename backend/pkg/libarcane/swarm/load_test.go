package swarm

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/stretchr/testify/require"
)

func TestLoadComposeProjectMergesOverrideContent(t *testing.T) {
	base := "services:\n  app:\n    image: nginx:alpine\n    environment:\n      FROM_BASE: \"1\"\n"
	override := "services:\n  app:\n    image: busybox:latest\n    environment:\n      FROM_OVERRIDE: \"1\"\n"

	project, err := projects.LoadComposeProjectFromContent(context.Background(), projecttypes.ComposeContentOptions{
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

func TestLoadComposeProjectWithoutOverrideContent(t *testing.T) {
	project, err := projects.LoadComposeProjectFromContent(context.Background(), projecttypes.ComposeContentOptions{
		ProjectName:    "stack",
		ComposeContent: "services:\n  app:\n    image: nginx:alpine\n",
	})
	require.NoError(t, err)
	require.Equal(t, "nginx:alpine", project.Services["app"].Image)
}

func TestLoadComposeProjectNamespacesResourceNames(t *testing.T) {
	composeContent := `
services:
  app:
    image: alpine
    volumes:
      - data:/data
      - named:/named
      - external:/external
    networks:
      - default
      - named
      - external
volumes:
  data: {}
  named:
    name: custom
  external:
    name: external
    external: true
networks:
  default: {}
  named:
    name: custom-network
  external:
    name: external-network
    external: true
`

	project, err := projects.LoadComposeProjectFromContent(context.Background(), projecttypes.ComposeContentOptions{
		ProjectName:    "stack",
		ComposeContent: composeContent,
	})
	require.NoError(t, err)

	require.Equal(t, "stack_data", project.Volumes["data"].Name)
	require.Equal(t, "custom", project.Volumes["named"].Name)
	require.Equal(t, "external", project.Volumes["external"].Name)
	require.Equal(t, "stack_default", project.Networks["default"].Name)
	require.Equal(t, "custom-network", project.Networks["named"].Name)
	require.Equal(t, "external-network", project.Networks["external"].Name)
}
