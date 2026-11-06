package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeComposeFile(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("services: {}\n"), 0o644))
}

func discoveredNames(dirs []DiscoveredProjectDir) []string {
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, d.DirName)
	}
	return out
}

func TestDiscoverProjectDirectories_StopsDescentAtCompose(t *testing.T) {
	root := t.TempDir()

	// A Compose file in networking owns both nested project directories.
	writeComposeFile(t, filepath.Join(root, "networking"))
	writeComposeFile(t, filepath.Join(root, "networking", "adguardhome"))
	writeComposeFile(t, filepath.Join(root, "networking", "nginx-proxy-manager"))

	discovered, err := DiscoverProjectDirectories(t.Context(), root, false, 0, "")
	require.NoError(t, err)

	names := discoveredNames(discovered)
	require.Equal(t, []string{"networking"}, names,
		"only the parent compose project should be discovered; children are assumed to be included")
}

func TestDiscoverProjectDirectories_SiblingProjectsAtRoot(t *testing.T) {
	root := t.TempDir()

	writeComposeFile(t, filepath.Join(root, "app1"))
	writeComposeFile(t, filepath.Join(root, "app2"))

	discovered, err := DiscoverProjectDirectories(t.Context(), root, false, 0, "")
	require.NoError(t, err)

	names := discoveredNames(discovered)
	require.ElementsMatch(t, []string{"app1", "app2"}, names)
}

func TestDiscoverProjectDirectories_RootWithComposeAndSiblings(t *testing.T) {
	root := t.TempDir()

	writeComposeFile(t, root)
	writeComposeFile(t, filepath.Join(root, "app1"))
	writeComposeFile(t, filepath.Join(root, "app2"))

	discovered := discoverProjectDirectories(t, root)

	names := discoveredNames(discovered)
	require.Len(t, names, 3)
	require.Contains(t, names, "app1")
	require.Contains(t, names, "app2")
	require.Contains(t, names, filepath.Base(root))
}

func TestDiscoverProjectDirectories_NestedStandaloneProject(t *testing.T) {
	root := t.TempDir()

	writeComposeFile(t, filepath.Join(root, "sub", "nested"))

	discovered, err := DiscoverProjectDirectories(t.Context(), root, false, 0, "")
	require.NoError(t, err)

	names := discoveredNames(discovered)
	require.Equal(t, []string{"nested"}, names)
}

func TestDiscoverProjectDirectories_SupportsCustomComposeFilename(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "Radarr-3")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "radarr.yaml"), []byte("services: {}\n"), 0o644))

	discovered, err := DiscoverProjectDirectories(t.Context(), root, false, 0, "")
	require.NoError(t, err)
	require.Len(t, discovered, 1)
	require.Equal(t, "Radarr-3", discovered[0].DirName)
	require.Equal(t, projectDir, discovered[0].Path)
}

func discoverProjectDirectories(t *testing.T, root string) []DiscoveredProjectDir {
	t.Helper()
	d, err := DiscoverProjectDirectories(t.Context(), root, false, 0, "")
	require.NoError(t, err)
	return d
}

func TestDiscoverProjectDirectories_RespectsMaxDepth(t *testing.T) {
	root := t.TempDir()

	writeComposeFile(t, filepath.Join(root, "top-level"))
	writeComposeFile(t, filepath.Join(root, "group", "nested"))

	discovered, err := DiscoverProjectDirectories(t.Context(), root, false, 1, "")
	require.NoError(t, err)

	names := discoveredNames(discovered)
	require.Equal(t, []string{"top-level"}, names)
}

func TestIsInternalScratchDirName(t *testing.T) {
	scratch := []string{
		".project-update-preview-123",
		".project-update-backup-123",
		".gitops-sync-stage-1219810203",
		".gitops-backup-456",
		"Makerra.gitops-backup-1780656786384743013", // legacy name-embedded form
		".arcane-trash-app-1234567890",              // quarantined (soft-deleted) project files
	}
	for _, name := range scratch {
		require.True(t, IsInternalScratchDirName(name), "expected %q to be an internal scratch dir", name)
	}

	notScratch := []string{
		"app",
		"Dozzle",
		"Dozzle-1",
		"my.gitops-backup-notes", // non-numeric tail: a real user project, not scratch
		"app.gitops-backup-2024", // short year-like tail: not a UnixNano scratch suffix
		"gitops-backup-1",        // no leading dot before the marker
	}
	for _, name := range notScratch {
		require.False(t, IsInternalScratchDirName(name), "expected %q NOT to be an internal scratch dir", name)
	}

	require.True(t, IsGitOpsScratchDirName(".gitops-sync-stage-1"))
	require.True(t, IsGitOpsScratchDirName("Makerra.gitops-backup-1780656786384743013"))
	require.False(t, IsGitOpsScratchDirName(".project-update-backup-1"))
}

func TestDiscoverProjectDirectories_SkipsGitOpsScratchDirs(t *testing.T) {
	root := t.TempDir()

	writeComposeFile(t, filepath.Join(root, "app"))
	writeComposeFile(t, filepath.Join(root, ".gitops-sync-stage-1219810203"))
	writeComposeFile(t, filepath.Join(root, ".gitops-backup-456"))
	writeComposeFile(t, filepath.Join(root, "Makerra.gitops-backup-1780656786384743013"))
	writeComposeFile(t, filepath.Join(root, "notes.gitops-backup-final"))

	names := discoveredNames(discoverProjectDirectories(t, root))
	require.ElementsMatch(t, []string{"app", "notes.gitops-backup-final"}, names)
}

func TestDiscoverProjectDirectories_SkipsArcaneTrashDirs(t *testing.T) {
	root := t.TempDir()

	writeComposeFile(t, filepath.Join(root, "app"))
	writeComposeFile(t, filepath.Join(root, ".arcane-trash-app-1234567890"))
	writeComposeFile(t, filepath.Join(root, ".hidden"))

	names := discoveredNames(discoverProjectDirectories(t, root))
	require.ElementsMatch(t, []string{"app", ".hidden"}, names)
}

func TestDiscoverProjectDirectories_SkipsUnreadableNestedSibling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission-denied behavior is not portable to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("test requires a non-root UID to trigger permission-denied on ReadDir")
	}

	root := t.TempDir()

	writeComposeFile(t, filepath.Join(root, "project1"))

	unreadableDir := filepath.Join(root, "adguard-cheetah", "workingdir")
	require.NoError(t, os.MkdirAll(unreadableDir, 0o755))
	t.Cleanup(func() { _ = os.Chmod(unreadableDir, 0o700) })
	require.NoError(t, os.Chmod(unreadableDir, 0))

	discovered, err := DiscoverProjectDirectories(t.Context(), root, false, 0, "")
	require.NoError(t, err, "discovery must not fail when only a nested, non-root directory is unreadable")

	// os.ReadDir returns entries sorted, so "adguard-cheetah" is walked before
	// "project1" alphabetically. Finding project1 in the results proves the
	// walk continued past the permission-denied failure rather than merely
	// tolerating a failure at the very end of the scan.
	assert.Equal(t, []string{"project1"}, discoveredNames(discovered))
}

func TestDiscoverProjectDirectories_SkipsFilesystemSnapshotDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeComposeFile(t, filepath.Join(root, "arcane"))
	writeComposeFile(t, filepath.Join(root, "#snapshot", "GMT+10-2026.07.04-03.30.01", "arcane"))
	writeComposeFile(t, filepath.Join(root, "#recycle", "arcane"))
	writeComposeFile(t, filepath.Join(root, ".snapshots", "1", "arcane"))

	discovered, err := DiscoverProjectDirectories(t.Context(), root, false, 0, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"arcane"}, discoveredNames(discovered))
}

func TestComposeContentProjectName(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"literal name", "name: sabnzbd\nservices:\n  sabnzbd:\n    image: nginx\n", "sabnzbd"},
		{"normalized", "name: My_App 2\nservices: {}\n", "my_app2"},
		{"absent", "services: {}\n", ""},
		{"interpolated", "name: ${PROJECT}\nservices: {}\n", ""},
		{"invalid yaml", "name: [unclosed\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			{
				got := ComposeContentProjectName(tt.content)
				assert.Equal(t, tt.want, got,
					"ComposeContentProjectName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeProjectName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple",
			input:    "myproject",
			expected: "myproject",
		},
		{
			name:     "with special chars",
			input:    "My Project!",
			expected: "myproject",
		},
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeProjectName(tt.input)

			require.Equal(t, tt.expected, got,
				"NormalizeProjectName() = %q, want %q", got, tt.expected)
		})
	}
}
