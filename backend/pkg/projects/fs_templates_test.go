package projects

import (
	"context"
	"os"
	"path/filepath"
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

			content, env, desc, found, err := ReadFolderComposeTemplate(t.Context(), baseDir, folder)
			require.NoError(t, err)
			require.True(t, found, "expected template to be detected for %s", tc.fileName)
			assert.Equal(t, composeContent, content)
			assert.Nil(t, env)
			assert.Contains(t, desc, tc.fileName, "description should reference detected filename, got %q", desc)
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

	_, env, _, found, err := ReadFolderComposeTemplate(t.Context(), baseDir, folder)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, env)
	assert.Equal(t, "KEY=value\n", *env)
}

func TestResolveTemplateIconURL(t *testing.T) {
	tests := []struct {
		name       string
		compose    string
		envContent string
		want       string
	}{
		{
			name: "top level icon",
			compose: `x-arcane:
  icon: https://cdn.example/icon.png
services:
  app:
    image: nginx:alpine
`,
			want: "https://cdn.example/icon.png",
		},
		{
			name: "icons alias",
			compose: `x-arcane:
  icons: https://cdn.example/alias.png
services:
  app:
    image: nginx:alpine
`,
			want: "https://cdn.example/alias.png",
		},
		{
			name: "env interpolation",
			compose: `x-arcane:
  icon: ${TEMPLATE_ICON}
services:
  app:
    image: nginx:alpine
`,
			envContent: "TEMPLATE_ICON=https://cdn.example/from-env.png\n",
			want:       "https://cdn.example/from-env.png",
		},
		{
			name: "invalid x arcane block",
			compose: `x-arcane: plain-text
services:
  app:
    image: nginx:alpine
`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iconURL := ResolveTemplateIconURL(context.Background(), tt.compose, tt.envContent)
			if tt.want == "" {
				require.Nil(t, iconURL)
				return
			}

			require.NotNil(t, iconURL)
			require.Equal(t, tt.want, *iconURL)
		})
	}
}
