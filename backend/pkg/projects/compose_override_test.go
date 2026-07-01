package projects

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadComposeOverrideContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	assert.Empty(t, ReadComposeOverrideContent(dir))

	content := "services:\n  app:\n    image: busybox:latest\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.override.yaml"), []byte(content), 0o600))
	assert.Equal(t, content, ReadComposeOverrideContent(dir))
}

func TestResolveComposeOverride(t *testing.T) {
	t.Parallel()

	t.Run("returns highest-preference match", func(t *testing.T) {
		t.Parallel()
		available := map[string]string{
			"compose.override.yml":         "first\n",
			"docker-compose.override.yaml": "second\n",
		}
		name, content, found, err := ResolveComposeOverride(
			func(n string) bool { _, ok := available[n]; return ok },
			func(n string) (string, error) { return available[n], nil },
		)
		require.NoError(t, err)
		require.True(t, found)
		// compose.override.yml precedes docker-compose.override.yaml in compose-go order.
		assert.Equal(t, "compose.override.yml", name)
		assert.Equal(t, "first\n", content)
	})

	t.Run("returns not found when nothing exists", func(t *testing.T) {
		t.Parallel()
		name, content, found, err := ResolveComposeOverride(
			func(string) bool { return false },
			func(string) (string, error) { return "", nil },
		)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, name)
		assert.Empty(t, content)
	})

	t.Run("propagates read error", func(t *testing.T) {
		t.Parallel()
		_, _, found, err := ResolveComposeOverride(
			func(string) bool { return true },
			func(string) (string, error) { return "", errors.New("boom") },
		)
		require.Error(t, err)
		assert.False(t, found)
	})
}

func TestWriteComposeOverrideFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "proj")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// A stale override under a different candidate name should be cleaned up.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.override.yml"), []byte("stale\n"), 0o600))

	content := "services:\n  app:\n    image: busybox:latest\n"
	require.NoError(t, WriteComposeOverrideFile(root, dir, &content, "compose.override.yaml"))

	written, err := os.ReadFile(filepath.Join(dir, "compose.override.yaml"))
	require.NoError(t, err)
	assert.Equal(t, content, string(written))

	_, statErr := os.Stat(filepath.Join(dir, "docker-compose.override.yml"))
	assert.True(t, os.IsNotExist(statErr))

	// Nil content removes all override files.
	require.NoError(t, WriteComposeOverrideFile(root, dir, nil, ""))
	_, statErr = os.Stat(filepath.Join(dir, "compose.override.yaml"))
	assert.True(t, os.IsNotExist(statErr))
}
