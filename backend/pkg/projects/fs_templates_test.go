package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFolderComposeTemplate_DetectsAllVariants(t *testing.T) {
	t.Parallel()

	composeContent := "services:\n  app:\n    image: nginx:alpine\n"

	testCases := []struct {
		name     string
		fileName string
	}{
		{name: "compose.yaml", fileName: "compose.yaml"},
		{name: "compose.yml", fileName: "compose.yml"},
		{name: "docker-compose.yml", fileName: "docker-compose.yml"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			baseDir := t.TempDir()
			folder := "demo"
			folderPath := filepath.Join(baseDir, folder)
			require.NoError(t, os.MkdirAll(folderPath, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(folderPath, tc.fileName), []byte(composeContent), 0o600))

			content, env, desc, found, err := ReadFolderComposeTemplate(baseDir, folder)
			require.NoError(t, err)
			require.True(t, found, "expected template to be detected for %s", tc.fileName)
			assert.Equal(t, composeContent, content)
			assert.Nil(t, env)
			assert.True(t, strings.Contains(desc, tc.fileName), "description should reference detected filename, got %q", desc)
		})
	}
}

func TestReadFolderComposeTemplate_ReadsEnvExample(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	folder := "envtest"
	folderPath := filepath.Join(baseDir, folder)
	require.NoError(t, os.MkdirAll(folderPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(folderPath, "compose.yml"), []byte("services: {}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(folderPath, ".env.example"), []byte("KEY=value\n"), 0o600))

	_, env, _, found, err := ReadFolderComposeTemplate(baseDir, folder)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, env)
	assert.Equal(t, "KEY=value\n", *env)
}
