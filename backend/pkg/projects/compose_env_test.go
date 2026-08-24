package projects

import (
	"os"
	"path/filepath"
	"testing"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseComposeEnvOptions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "base.yml"), []byte("services: {}\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "extra.yml"), []byte("services: {}\n"), 0o600))

	t.Run("scalars, profiles split, and custom separator", func(t *testing.T) {
		t.Parallel()
		env := EnvMap{
			"COMPOSE_FILE":           "base.yml;sub/extra.yml",
			"COMPOSE_PATH_SEPARATOR": ";",
			"COMPOSE_PROFILES":       "frontend, debug",
			"COMPOSE_PROJECT_NAME":   "myproj",
			"COMPOSE_REMOVE_ORPHANS": "true",
			"COMPOSE_IGNORE_ORPHANS": "1",
			"COMPOSE_PARALLEL_LIMIT": "4",
		}

		opts, err := ParseComposeEnvOptions(dir, env)
		require.NoError(t, err)
		assert.Equal(t, []string{filepath.Join(dir, "base.yml"), filepath.Join(dir, "sub", "extra.yml")}, opts.ConfigFiles)
		assert.Equal(t, []string{"frontend", "debug"}, opts.Profiles)
		assert.Equal(t, "myproj", opts.ProjectName)
		assert.True(t, opts.RemoveOrphans)
		assert.True(t, opts.IgnoreOrphans)
		assert.Equal(t, 4, opts.ParallelLimit)
	})

	t.Run("invalid scalars are ignored", func(t *testing.T) {
		t.Parallel()
		opts, err := ParseComposeEnvOptions(dir, EnvMap{
			"COMPOSE_REMOVE_ORPHANS": "nope",
			"COMPOSE_PARALLEL_LIMIT": "-3",
		})
		require.NoError(t, err)
		assert.False(t, opts.RemoveOrphans)
		assert.Zero(t, opts.ParallelLimit)
	})

	t.Run("COMPOSE_ENV_FILES resolved in order, traversal rejected", func(t *testing.T) {
		t.Parallel()
		opts, err := ParseComposeEnvOptions(dir, EnvMap{"COMPOSE_ENV_FILES": "sub/extra.env, .env.local"})
		require.NoError(t, err)
		assert.Equal(t, []string{filepath.Join(dir, "sub", "extra.env"), filepath.Join(dir, ".env.local")}, opts.EnvFiles)

		_, err = ParseComposeEnvOptions(dir, EnvMap{"COMPOSE_ENV_FILES": "../outside.env"})
		require.Error(t, err)
		assert.True(t, errors.Is(err, common.ErrComposeFileEnvInvalid))
	})

	t.Run("absolute entry rejected", func(t *testing.T) {
		t.Parallel()
		_, err := ParseComposeEnvOptions(dir, EnvMap{"COMPOSE_FILE": filepath.Join(dir, "base.yml")})
		require.Error(t, err)
		assert.True(t, errors.Is(err, common.ErrComposeFileEnvInvalid))
	})

	t.Run("escaping entry rejected", func(t *testing.T) {
		t.Parallel()
		_, err := ParseComposeEnvOptions(dir, EnvMap{"COMPOSE_FILE": "../base.yml"})
		require.Error(t, err)
		assert.True(t, errors.Is(err, common.ErrComposeFileEnvInvalid))
	})

	t.Run("non-root first entry rejected", func(t *testing.T) {
		t.Parallel()
		_, err := ParseComposeEnvOptions(dir, EnvMap{"COMPOSE_FILE": "sub/extra.yml:base.yml"})
		require.Error(t, err)
		assert.True(t, errors.Is(err, common.ErrComposeFileEnvInvalid))
	})

	t.Run("missing entry rejected", func(t *testing.T) {
		t.Parallel()
		_, err := ParseComposeEnvOptions(dir, EnvMap{"COMPOSE_FILE": "base.yml:sub/missing.yml"})
		require.Error(t, err)
		assert.True(t, errors.Is(err, common.ErrComposeFileEnvInvalid))
	})
}

func TestComposeFileEnvSelection(t *testing.T) {
	t.Parallel()

	t.Run("nil when no .env", func(t *testing.T) {
		t.Parallel()
		files, err := ComposeFileEnvSelection(t.Context(), "", t.TempDir())
		require.NoError(t, err)
		assert.Nil(t, files)
	})

	t.Run("resolves COMPOSE_FILE from .env", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "base.yml"), []byte("services: {}\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.yml"), []byte("services: {}\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("COMPOSE_FILE=base.yml:extra.yml\n"), 0o600))

		files, err := ComposeFileEnvSelection(t.Context(), "", dir)
		require.NoError(t, err)
		assert.Equal(t, []string{filepath.Join(dir, "base.yml"), filepath.Join(dir, "extra.yml")}, files)
	})

	t.Run("resolves COMPOSE_FILE from COMPOSE_ENV_FILES entry", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "base.yml"), []byte("services: {}\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "aux.env"), []byte("COMPOSE_FILE=base.yml\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("COMPOSE_ENV_FILES=aux.env\n"), 0o600))

		files, err := ComposeFileEnvSelection(t.Context(), "", dir)
		require.NoError(t, err)
		assert.Equal(t, []string{filepath.Join(dir, "base.yml")}, files)
	})

	t.Run("global COMPOSE_DISABLE_ENV_FILE skips project .env", func(t *testing.T) {
		t.Parallel()
		projectsDir := t.TempDir()
		dir := filepath.Join(projectsDir, "proj")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(projectsDir, GlobalEnvFileName), []byte("COMPOSE_DISABLE_ENV_FILE=true\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "base.yml"), []byte("services: {}\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("COMPOSE_FILE=base.yml\n"), 0o600))

		files, err := ComposeFileEnvSelection(t.Context(), projectsDir, dir)
		require.NoError(t, err)
		assert.Nil(t, files)
	})

	t.Run("global COMPOSE_FILE selects the base when the project .env is silent", func(t *testing.T) {
		t.Parallel()
		projectsDir := t.TempDir()
		dir := filepath.Join(projectsDir, "proj")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(projectsDir, GlobalEnvFileName), []byte("COMPOSE_FILE=base.yml\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "base.yml"), []byte("services: {}\n"), 0o600))

		files, err := ComposeFileEnvSelection(t.Context(), projectsDir, dir)
		require.NoError(t, err)
		assert.Equal(t, []string{filepath.Join(dir, "base.yml")}, files)
	})

	t.Run("project .env COMPOSE_FILE overrides the global value", func(t *testing.T) {
		t.Parallel()
		projectsDir := t.TempDir()
		dir := filepath.Join(projectsDir, "proj")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(projectsDir, GlobalEnvFileName), []byte("COMPOSE_FILE=global.yml\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "base.yml"), []byte("services: {}\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "global.yml"), []byte("services: {}\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("COMPOSE_FILE=base.yml\n"), 0o600))

		files, err := ComposeFileEnvSelection(t.Context(), projectsDir, dir)
		require.NoError(t, err)
		assert.Equal(t, []string{filepath.Join(dir, "base.yml")}, files)
	})

	t.Run("COMPOSE_DISABLE_ENV_FILE still honors a global COMPOSE_FILE", func(t *testing.T) {
		t.Parallel()
		projectsDir := t.TempDir()
		dir := filepath.Join(projectsDir, "proj")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(projectsDir, GlobalEnvFileName), []byte("COMPOSE_DISABLE_ENV_FILE=true\nCOMPOSE_FILE=base.yml\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "base.yml"), []byte("services: {}\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("COMPOSE_FILE=other.yml\n"), 0o600))

		files, err := ComposeFileEnvSelection(t.Context(), projectsDir, dir)
		require.NoError(t, err)
		assert.Equal(t, []string{filepath.Join(dir, "base.yml")}, files)
	})
}
