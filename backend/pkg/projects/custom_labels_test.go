package projects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseArcaneComposeMetadata_InterpolationAndAnchor(t *testing.T) {
	tempDir := t.TempDir()

	envContent := "ARCANE_TEST_DOMAIN=example.com\nARCANE_TEST_ICONS_CDN=https://cdn.jsdelivr.net/gh/homarr-labs\n"
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0o600))

	composeContent := `services:
  app:
    image: nginx:alpine
x-arcane-icon: &arcane-icon "${ARCANE_TEST_ICONS_CDN}/webp/raspberry-pi.webp"
x-arcane:
  icon: *arcane-icon
  urls:
    - https://www.${ARCANE_TEST_DOMAIN}
`

	composePath := filepath.Join(tempDir, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte(composeContent), 0o600))

	meta, err := ParseArcaneComposeMetadata(context.Background(), composePath)
	require.NoError(t, err)
	require.Equal(t, "https://cdn.jsdelivr.net/gh/homarr-labs/webp/raspberry-pi.webp", meta.ProjectIconURL)
	require.Equal(t, []string{"https://www.example.com"}, meta.ProjectURLS)
}

func TestParseArcaneComposeMetadata_IncludeSupport(t *testing.T) {
	tempDir := t.TempDir()

	composeContent := `include:
  - meta.yaml
services:
  app:
    image: nginx:alpine
`
	composePath := filepath.Join(tempDir, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte(composeContent), 0o600))

	metaContent := `x-arcane:
  icon: https://example.com/icon.png
  urls:
    - https://example.com/docs
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "meta.yaml"), []byte(metaContent), 0o600))

	meta, err := ParseArcaneComposeMetadata(context.Background(), composePath)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/icon.png", meta.ProjectIconURL)
	require.Equal(t, []string{"https://example.com/docs"}, meta.ProjectURLS)
}
