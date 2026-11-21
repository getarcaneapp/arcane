package projects

import (
	"os"
	"path/filepath"
	"testing"

	composeTypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildImageRefsFromComposeProject(t *testing.T) {
	project := &composeTypes.Project{
		Name: "demo",
		Services: composeTypes.Services{
			"regular": {
				Name:  "regular",
				Image: "postgres:17",
			},
			"explicit": {
				Name:  "explicit",
				Image: "test2:latest",
				Build: &composeTypes.BuildConfig{Context: "."},
			},
			"duplicate": {
				Name:  "duplicate",
				Image: "test2:latest",
				Build: &composeTypes.BuildConfig{Context: "./duplicate"},
			},
			"default": {
				Build: &composeTypes.BuildConfig{Context: "./default"},
			},
		},
	}

	assert.Equal(t, []string{"demo-default", "test2:latest"}, BuildImageRefsFromComposeProject(project))
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

	project, err := LoadComposeProject(t.Context(), LoadOptions{
		ComposeFile:       composePath,
		ProjectName:       "override-demo",
		ProjectsDirectory: dir,
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"overridden-app:latest"}, BuildImageRefsFromComposeProject(project))
}
