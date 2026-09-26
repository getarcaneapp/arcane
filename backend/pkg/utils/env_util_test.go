package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeSecretFileInternal(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLookupEnvOrFile(t *testing.T) {
	t.Run("file stands in for the variable, trimmed", func(t *testing.T) {
		t.Setenv("ARCANE_TEST_SECRET_FILE", writeSecretFileInternal(t, "single", "from-file\n"))
		t.Setenv("ARCANE_TEST_SECRET", "from-env")

		value, ok := LookupEnvOrFile("ARCANE_TEST_SECRET")
		require.True(t, ok)
		require.Equal(t, "from-file", value)
	})

	t.Run("double underscore takes precedence", func(t *testing.T) {
		t.Setenv("ARCANE_TEST_SECRET__FILE", writeSecretFileInternal(t, "double", "double"))
		t.Setenv("ARCANE_TEST_SECRET_FILE", writeSecretFileInternal(t, "single", "single"))

		value, ok := LookupEnvOrFile("ARCANE_TEST_SECRET")
		require.True(t, ok)
		require.Equal(t, "double", value)
	})

	t.Run("falls back to the variable without a file", func(t *testing.T) {
		t.Setenv("ARCANE_TEST_SECRET", "from-env")

		value, ok := LookupEnvOrFile("ARCANE_TEST_SECRET")
		require.True(t, ok)
		require.Equal(t, "from-env", value)
	})

	t.Run("falls back to the variable when the file is unreadable", func(t *testing.T) {
		t.Setenv("ARCANE_TEST_SECRET_FILE", filepath.Join(t.TempDir(), "missing"))
		t.Setenv("ARCANE_TEST_SECRET", "from-env")

		value, ok := LookupEnvOrFile("ARCANE_TEST_SECRET")
		require.True(t, ok)
		require.Equal(t, "from-env", value)
	})

	t.Run("reports absence", func(t *testing.T) {
		_, ok := LookupEnvOrFile("ARCANE_TEST_SECRET_UNSET")
		require.False(t, ok)
	})
}
