package projects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	updaterlabels "go.getarcane.app/updater/labels"

	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
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

func TestParseArcaneComposeMetadata_TagNamesColorsInterpolationAndIncludes(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, ".env"), []byte("PROJECT_KIND=Database\nPROJECT_COLOR=Purple\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "compose.yaml"), []byte(`include:
  - included.yaml
x-arcane:
  tags:
    - name: ${PROJECT_KIND}
      color: ${PROJECT_COLOR}
services:
  app:
    image: nginx:alpine
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "included.yaml"), []byte(`x-arcane:
  tags:
    - name: maintenance-window
      color: orange
    - name: DATABASE
      color: red
    - name: invalid-color
      color: chartreuse
    - name: missing-color
services: {}
`), 0o600))

	meta, err := ParseArcaneComposeMetadata(context.Background(), filepath.Join(tempDir, "compose.yaml"), tempDir, false)
	require.NoError(t, err)
	require.Equal(t, []projecttypes.TagOption{
		{Name: "database", Color: projecttypes.TagColorPurple},
		{Name: "maintenance-window", Color: projecttypes.TagColorOrange},
	}, meta.ProjectTags)
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

func TestNormalizeProjectTags(t *testing.T) {
	tags, err := NormalizeProjectTags([]string{" Database ", "DATABASE", "maintenance-window"})
	require.NoError(t, err)
	require.Equal(t, []string{"database", "maintenance-window"}, tags)

	for _, invalid := range []string{"", "bad,tag", "bad\ntag", strings.Repeat("a", ProjectTagMaxLength+1)} {
		_, err := NormalizeProjectTag(invalid)
		require.Error(t, err, invalid)
	}

	tooMany := make([]string, ProjectTagsPerSourceLimit+1)
	for index := range tooMany {
		tooMany[index] = "tag-" + strings.Repeat("x", index/10) + string(rune('a'+index%10))
	}
	_, err = NormalizeProjectTags(tooMany)
	require.Error(t, err)
}

func TestUpdaterMetadataLabelsInternal(t *testing.T) {
	const content = `x-arcane:
  icon: alpine
  updater:
    enabled: true
    strategy: tag
    constraint: "3.x"
    tag-pattern: 'v?(?P<version>\d+\.\d+\.\d+)'
services:
  inherited:
    image: alpine:3.20.0
  overridden:
    image: alpine:3.20.0
    x-arcane:
      updater:
        constraint: "=3.20.1"
        enabled: false
  explicit:
    image: alpine:3.20.0
    x-arcane:
      updater:
        constraint: "=3.20.1"
    labels:
      com.getarcaneapp.arcane.updater.strategy: digest
      com.getarcaneapp.arcane.updater.constraint: ""
      custom: keep
`
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	disk, err := LoadComposeProject(t.Context(), path, "metadata", dir, false, nil, nil, nil, false, nil, nil)
	require.NoError(t, err)
	memory, err := LoadComposeProjectFromContent(t.Context(), projecttypes.ComposeContentOptions{ComposeContent: content, WorkingDir: dir, ProjectName: "metadata"})
	require.NoError(t, err)
	for _, project := range []*composetypes.Project{disk, memory} {
		inherited := project.Services["inherited"].Labels
		require.Equal(t, "tag", inherited[updaterlabels.LabelUpdateStrategy])
		require.Equal(t, "3.x", inherited[updaterlabels.LabelUpdateConstraint])
		require.Equal(t, "true", inherited[updaterlabels.LabelUpdater])
		overridden := project.Services["overridden"].Labels
		require.Equal(t, "=3.20.1", overridden[updaterlabels.LabelUpdateConstraint])
		require.Equal(t, "false", overridden[updaterlabels.LabelUpdater])
		require.Equal(t, inherited[updaterlabels.LabelUpdateTagPattern], overridden[updaterlabels.LabelUpdateTagPattern])
		explicit := project.Services["explicit"].Labels
		require.Equal(t, "digest", explicit[updaterlabels.LabelUpdateStrategy])
		require.Equal(t, "", explicit[updaterlabels.LabelUpdateConstraint])
		require.Equal(t, "keep", explicit["custom"])
	}
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, string(source), "loading must not rewrite source into labels")
}

func TestUpdaterMetadataValidationInternal(t *testing.T) {
	for _, config := range []string{"updater: true", "updater: {enabled: maybe}", "updater: {strategy: newest}", "updater: {constraint: 123}", "updater: {tag-pattern: []}", "updater: {unknown: true}"} {
		t.Run(config, func(t *testing.T) {
			_, err := LoadComposeProjectFromContent(t.Context(), projecttypes.ComposeContentOptions{ComposeContent: "services:\n  app:\n    image: alpine:3.20.0\nx-arcane:\n  " + config + "\n", WorkingDir: t.TempDir(), ProjectName: "metadata"})
			require.Error(t, err)
			require.Contains(t, err.Error(), "updater")
		})
	}
}

func TestUpdaterMetadataInterpolationAndIncludesInternal(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("UPDATE_RANGE=3.20.x\nUPDATE_ENABLED=false\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`include:
  - included.yaml
x-arcane:
  updater:
    strategy: tag
    constraint: "${UPDATE_RANGE}"
    enabled: "${UPDATE_ENABLED}"
services:
  main:
    image: alpine:3.20.0
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "included.yaml"), []byte(`services:
  child:
    image: alpine:3.20.0
    x-arcane:
      updater:
        enabled: true
        constraint: "=3.20.1"
`), 0o600))
	project, err := LoadComposeProject(t.Context(), filepath.Join(dir, "compose.yaml"), "metadata", dir, false, nil, nil, nil, false, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "3.20.x", project.Services["main"].Labels[updaterlabels.LabelUpdateConstraint])
	require.Equal(t, "false", project.Services["main"].Labels[updaterlabels.LabelUpdater])
	require.Equal(t, "tag", project.Services["child"].Labels[updaterlabels.LabelUpdateStrategy])
	require.Equal(t, "=3.20.1", project.Services["child"].Labels[updaterlabels.LabelUpdateConstraint])
	require.Equal(t, "true", project.Services["child"].Labels[updaterlabels.LabelUpdater])
}
