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
x-arcane-icon-light: &arcane-icon "${ARCANE_TEST_ICONS_CDN}/webp/raspberry-pi.webp"
x-arcane:
  icon-light: *arcane-icon
  icon-dark: *arcane-icon
  urls:
    - https://www.${ARCANE_TEST_DOMAIN}
`

	composePath := filepath.Join(tempDir, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte(composeContent), 0o600))

	meta, err := ParseArcaneComposeMetadata(context.Background(), composePath, tempDir, false)
	require.NoError(t, err)
	require.Equal(t, "https://cdn.jsdelivr.net/gh/homarr-labs/webp/raspberry-pi.webp", meta.ProjectIcon.Light)
	require.Equal(t, "https://cdn.jsdelivr.net/gh/homarr-labs/webp/raspberry-pi.webp", meta.ProjectIcon.Dark)
	require.Equal(t, []string{"https://www.example.com"}, meta.ProjectURLS)
}

func TestParseArcaneComposeMetadata_ServiceLabelIconsOutrankProjectIcon(t *testing.T) {
	tempDir := t.TempDir()

	composeContent := `x-arcane:
  icon-light: umami
  icon-dark: umami
services:
  umami:
    image: ghcr.io/umami-software/umami:latest
  db:
    image: postgres:16
    labels:
      com.getarcaneapp.arcane.icon-light: postgres
      com.getarcaneapp.arcane.icon-dark: postgres
`
	composePath := filepath.Join(tempDir, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte(composeContent), 0o600))

	meta, err := ParseArcaneComposeMetadata(context.Background(), composePath, tempDir, false)
	require.NoError(t, err)
	require.Equal(t, IconSet{Light: "postgres", Dark: "postgres"}, meta.ServiceIconSets["db"])
	require.NotContains(t, meta.ServiceIconSets, "umami")
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
  icon-light: https://example.com/icon-light.png
  icon-dark: https://example.com/icon-dark.png
  urls:
    - https://example.com/docs
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "meta.yaml"), []byte(metaContent), 0o600))

	meta, err := ParseArcaneComposeMetadata(context.Background(), composePath, tempDir, false)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/icon-light.png", meta.ProjectIcon.Light)
	require.Equal(t, "https://example.com/icon-dark.png", meta.ProjectIcon.Dark)
	require.Equal(t, []string{"https://example.com/docs"}, meta.ProjectURLS)
}

func TestParseArcaneComposeMetadata_LoadsGlobalEnvForIncludedMetadata(t *testing.T) {
	projectsRoot := t.TempDir()
	projectDir := filepath.Join(projectsRoot, "demo")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	require.NoError(t, os.WriteFile(
		filepath.Join(projectsRoot, GlobalEnvFileName),
		[]byte("ICON_CDN_URL=https://cdn.jsdelivr.net/gh/selfhst/icons@main\n"),
		0o600,
	))

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte(`include:
  - metadata.yaml
services:
  watchtower:
    image: nickfedor/watchtower:latest
`), 0o600))

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "metadata.yaml"), []byte(`x-watchtower-icon-light: &watchtower-icon "${ICON_CDN_URL:+${ICON_CDN_URL}/svg/watchtower.svg}"
x-arcane:
  icon-light: *watchtower-icon
  icon-dark: *watchtower-icon
`), 0o600))

	meta, err := ParseArcaneComposeMetadata(context.Background(), filepath.Join(projectDir, "compose.yaml"), projectsRoot, false)
	require.NoError(t, err)
	require.Equal(t, "https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/watchtower.svg", meta.ProjectIcon.Light)
	require.Equal(t, "https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/watchtower.svg", meta.ProjectIcon.Dark)
}

func TestParseArcaneComposeMetadata_LoadsGlobalEnvForNestedProjects(t *testing.T) {
	projectsRoot := t.TempDir()
	projectDir := filepath.Join(projectsRoot, "group", "demo")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	require.NoError(t, os.WriteFile(
		filepath.Join(projectsRoot, GlobalEnvFileName),
		[]byte("ICON_CDN_URL=https://cdn.jsdelivr.net/gh/selfhst/icons@main\n"),
		0o600,
	))

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte(`include:
  - metadata.yaml
services:
  watchtower:
    image: nickfedor/watchtower:latest
`), 0o600))

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "metadata.yaml"), []byte(`x-watchtower-icon-light: &watchtower-icon "${ICON_CDN_URL:+${ICON_CDN_URL}/svg/watchtower.svg}"
x-arcane:
  icon-light: *watchtower-icon
  icon-dark: *watchtower-icon
`), 0o600))

	meta, err := ParseArcaneComposeMetadata(context.Background(), filepath.Join(projectDir, "compose.yaml"), projectsRoot, false)
	require.NoError(t, err)
	require.Equal(t, "https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/watchtower.svg", meta.ProjectIcon.Light)
	require.Equal(t, "https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/watchtower.svg", meta.ProjectIcon.Dark)
}
