package projects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildImageRefsFromComposeProject(t *testing.T) {
	project := &composetypes.Project{
		Name: "demo",
		Services: composetypes.Services{
			"regular": {
				Name:  "regular",
				Image: "postgres:17",
			},
			"explicit": {
				Name:  "explicit",
				Image: "test2:latest",
				Build: &composetypes.BuildConfig{Context: "."},
			},
			"duplicate": {
				Name:  "duplicate",
				Image: "test2:latest",
				Build: &composetypes.BuildConfig{Context: "./duplicate"},
			},
			"default": {
				Build: &composetypes.BuildConfig{Context: "./default"},
			},
		},
	}

	assert.Equal(t, []string{"demo-default", "test2:latest"}, BuildImageRefsFromComposeProject(project))
}

func TestBuildImageRefsFromComposeProject_NilProject(t *testing.T) {
	assert.Nil(t, BuildImageRefsFromComposeProject(nil))
}

func TestBuildImageRefsFromComposeProject_UsesMergedComposeOverrides(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte(`services:
  app:
    image: overridden-app:latest
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.override.yaml"), []byte(`services:
  app:
    build: ./app
`), 0o644))

	project, err := LoadComposeProject(context.Background(), composePath, "override-demo", dir, false, nil)
	require.NoError(t, err)

	assert.Equal(t, []string{"overridden-app:latest"}, BuildImageRefsFromComposeProject(project))
}
