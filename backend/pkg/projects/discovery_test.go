package projects

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeComposeFile creates an empty compose.yml at the given directory.
func writeComposeFile(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("services: {}\n"), 0o644))
}

// discoveredNames returns the DirName of each discovered project, preserving order.
func discoveredNames(dirs []DiscoveredProjectDir) []string {
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, d.DirName)
	}
	return out
}

// TestDiscoverProjectDirectories_StopsDescentAtCompose verifies that once a
// compose file is found at a given level, children are not discovered as
// separate top-level projects (they are assumed to belong to the parent,
// e.g. via compose include: directives).
func TestDiscoverProjectDirectories_StopsDescentAtCompose(t *testing.T) {
	root := t.TempDir()

	// Layout:
	//   root/networking/compose.yml
	//   root/networking/adguardhome/compose.yml
	//   root/networking/nginx-proxy-manager/compose.yml
	writeComposeFile(t, filepath.Join(root, "networking"))
	writeComposeFile(t, filepath.Join(root, "networking", "adguardhome"))
	writeComposeFile(t, filepath.Join(root, "networking", "nginx-proxy-manager"))

	discovered, err := DiscoverProjectDirectories(root, false)
	require.NoError(t, err)

	names := discoveredNames(discovered)
	require.Equal(t, []string{"networking"}, names,
		"only the parent compose project should be discovered; children are assumed to be included")
}

// TestDiscoverProjectDirectories_SiblingProjectsAtRoot verifies that multiple
// sibling projects directly under the root are all discovered when the root
// itself has no compose file.
func TestDiscoverProjectDirectories_SiblingProjectsAtRoot(t *testing.T) {
	root := t.TempDir()

	writeComposeFile(t, filepath.Join(root, "app1"))
	writeComposeFile(t, filepath.Join(root, "app2"))

	discovered, err := DiscoverProjectDirectories(root, false)
	require.NoError(t, err)

	names := discoveredNames(discovered)
	require.ElementsMatch(t, []string{"app1", "app2"}, names)
}

// TestDiscoverProjectDirectories_RootWithComposeAndSiblings verifies the
// projects root directory is exempt from the stop-at-compose rule, so siblings
// under the root are still discovered even if the root itself contains a
// compose file.
func TestDiscoverProjectDirectories_RootWithComposeAndSiblings(t *testing.T) {
	root := t.TempDir()

	// root/compose.yml (a root-level "project")
	// root/app1/compose.yml
	// root/app2/compose.yml
	writeComposeFile(t, root)
	writeComposeFile(t, filepath.Join(root, "app1"))
	writeComposeFile(t, filepath.Join(root, "app2"))

	discovered := DiscoveredProjectDirectories_call(t, root)

	names := discoveredNames(discovered)
	require.Len(t, names, 3)
	require.Contains(t, names, "app1")
	require.Contains(t, names, "app2")
	// The root project is discovered using the base name of the temp dir.
	require.Contains(t, names, filepath.Base(root))
}

// TestDiscoverProjectDirectories_NestedStandaloneProject verifies that a
// deeply nested compose file (with no intermediate compose files) is still
// discovered.
func TestDiscoverProjectDirectories_NestedStandaloneProject(t *testing.T) {
	root := t.TempDir()

	// root/sub/nested/compose.yml (no compose file at root/sub/)
	writeComposeFile(t, filepath.Join(root, "sub", "nested"))

	discovered, err := DiscoverProjectDirectories(root, false)
	require.NoError(t, err)

	names := discoveredNames(discovered)
	require.Equal(t, []string{"nested"}, names)
}

// DiscoveredProjectDirectories_call is a tiny helper so tests can share the
// err-check boilerplate.
func DiscoveredProjectDirectories_call(t *testing.T, root string) []DiscoveredProjectDir {
	t.Helper()
	d, err := DiscoverProjectDirectories(root, false)
	require.NoError(t, err)
	return d
}
