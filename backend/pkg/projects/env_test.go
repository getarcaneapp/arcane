package projects

import (
	"context"
	stderrors "errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEnvironment(t *testing.T) {
	// Setup temp dirs
	tmpDir := t.TempDir()
	projectsDir := filepath.Join(tmpDir, "projects")
	workdir := filepath.Join(projectsDir, "myproject")

	err := os.MkdirAll(workdir, utils.DirPerm)
	require.NoError(t, err)

	// Create .env.global
	globalEnvContent := "GLOBAL_VAR=global_value\nSHARED_VAR=global_shared"
	err = os.WriteFile(filepath.Join(projectsDir, ".env.global"), []byte(globalEnvContent), utils.FilePerm)
	require.NoError(t, err)

	// Create .env
	projectEnvContent := "PROJECT_VAR=project_value\nSHARED_VAR=project_shared"
	err = os.WriteFile(filepath.Join(workdir, ".env"), []byte(projectEnvContent), utils.FilePerm)
	require.NoError(t, err)

	t.Run("AutoInjectEnv=false", func(t *testing.T) {
		loader := NewEnvLoader(projectsDir, workdir, false)
		ctx := context.Background()

		envMap, injectionVars, err := loader.LoadEnvironment(ctx)
		require.NoError(t, err)

		// Verify envMap (should contain all vars, project overrides global)
		assert.Equal(t, "global_value", envMap["GLOBAL_VAR"])
		assert.Equal(t, "project_value", envMap["PROJECT_VAR"])
		assert.Equal(t, "project_shared", envMap["SHARED_VAR"])

		// Verify injectionVars (should ONLY contain global vars)
		assert.Equal(t, "global_value", injectionVars["GLOBAL_VAR"])
		assert.Equal(t, "global_shared", injectionVars["SHARED_VAR"])

		_, projectVarInInjection := injectionVars["PROJECT_VAR"]
		assert.False(t, projectVarInInjection, "Project variable should not be in injectionVars")
	})

	t.Run("AutoInjectEnv=true", func(t *testing.T) {
		loader := NewEnvLoader(projectsDir, workdir, true)
		ctx := context.Background()

		envMap, injectionVars, err := loader.LoadEnvironment(ctx)
		require.NoError(t, err)

		// Verify envMap
		assert.Equal(t, "global_value", envMap["GLOBAL_VAR"])
		assert.Equal(t, "project_value", envMap["PROJECT_VAR"])
		assert.Equal(t, "project_shared", envMap["SHARED_VAR"])

		// Verify injectionVars (should contain both global and project vars)
		assert.Equal(t, "global_value", injectionVars["GLOBAL_VAR"])
		assert.Equal(t, "project_value", injectionVars["PROJECT_VAR"])
		assert.Equal(t, "project_shared", injectionVars["SHARED_VAR"])
	})
}

func TestLoadEnvironment_DoesNotCreateMissingGlobalEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	projectsDir := filepath.Join(tmpDir, "projects")
	workdir := filepath.Join(projectsDir, "myproject")

	require.NoError(t, os.MkdirAll(workdir, utils.DirPerm))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, ".env"), []byte("PROJECT_VAR=project_value\n"), utils.FilePerm))

	loader := NewEnvLoader(projectsDir, workdir, false)
	ctx := context.Background()

	envMap, injectionVars, err := loader.LoadEnvironment(ctx)
	require.NoError(t, err)

	assert.Equal(t, "project_value", envMap["PROJECT_VAR"])
	assert.Empty(t, injectionVars)

	_, statErr := os.Stat(filepath.Join(projectsDir, GlobalEnvFileName))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestWithTransientValidationEnvFile_PreservesExternalSymlink(t *testing.T) {
	projectDir := t.TempDir()
	targetPath := filepath.Join(t.TempDir(), "project.env")
	originalContent := "VALUE=original\n"
	targetPerm := os.FileMode(0o640)
	require.NoError(t, os.WriteFile(targetPath, []byte(originalContent), targetPerm))
	require.NoError(t, os.Chmod(targetPath, targetPerm))

	envPath := filepath.Join(projectDir, EffectiveEnvFileName)
	if err := os.Symlink(targetPath, envPath); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	originalLinkTarget, err := os.Readlink(envPath)
	require.NoError(t, err)

	validationErr := stderrors.New("validation failed")
	updatedContent := "VALUE=validation\n"
	err = WithTransientValidationEnvFile(t.Context(), projectDir, &updatedContent, func() error {
		content, readErr := os.ReadFile(targetPath)
		require.NoError(t, readErr)
		assert.Equal(t, updatedContent, string(content))

		linkInfo, statErr := os.Lstat(envPath)
		require.NoError(t, statErr)
		require.NotZero(t, linkInfo.Mode()&os.ModeSymlink)
		return validationErr
	})
	require.ErrorIs(t, err, validationErr)

	restoredContent, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, originalContent, string(restoredContent))
	currentLinkTarget, err := os.Readlink(envPath)
	require.NoError(t, err)
	assert.Equal(t, originalLinkTarget, currentLinkTarget)
	if runtime.GOOS != "windows" {
		targetInfo, statErr := os.Stat(targetPath)
		require.NoError(t, statErr)
		assert.Equal(t, targetPerm, targetInfo.Mode().Perm())
	}
}

func TestBuildEffectiveEnvContent(t *testing.T) {
	tests := []struct {
		name            string
		gitContent      string
		overrideContent string
		want            string
		wantEnv         EnvMap
	}{
		{
			name:       "preserves escaped dollar secret exactly",
			gitContent: "CLOUDFLARE_CLIENT_SECRET=$$pbkdf2-sha512$$310000$$XXX\n",
			want:       "CLOUDFLARE_CLIENT_SECRET=$$pbkdf2-sha512$$310000$$XXX\n",
			wantEnv:    EnvMap{"CLOUDFLARE_CLIENT_SECRET": "$pbkdf2-sha512$310000$XXX"},
		},
		{
			name:       "preserves single quoted dollar secret exactly",
			gitContent: "CLOUDFLARE_CLIENT_SECRET='$pbkdf2-sha512$310000$XXX'",
			want:       "CLOUDFLARE_CLIENT_SECRET='$pbkdf2-sha512$310000$XXX'",
			wantEnv:    EnvMap{"CLOUDFLARE_CLIENT_SECRET": "$pbkdf2-sha512$310000$XXX"},
		},
		{
			name:       "preserves comments ordering blank lines bom and crlf",
			gitContent: "\ufeff# keep this comment\r\nZ_LAST=last\r\n\r\nA_FIRST=first",
			want:       "\ufeff# keep this comment\r\nZ_LAST=last\r\n\r\nA_FIRST=first",
			wantEnv:    EnvMap{"Z_LAST": "last", "A_FIRST": "first"},
		},
		{
			name:            "rewrites shared keys in place and appends override-only keys",
			gitContent:      "BASE_URL=https://example.com\nSHARED=git",
			overrideContent: "# local values\nAPI_TOKEN=secret\nSHARED=override\n",
			want:            "BASE_URL=https://example.com\nSHARED=override\n# local values\nAPI_TOKEN=secret\n",
			wantEnv: EnvMap{
				"BASE_URL":  "https://example.com",
				"API_TOKEN": "secret",
				"SHARED":    "override",
			},
		},
		{
			name:            "updates overridden value in place preserving inline comment",
			gitContent:      "PORT=3001 # Port to run the server on\n",
			overrideContent: "PORT=3011\n",
			want:            "PORT=3011 # Port to run the server on\n",
			wantEnv:         EnvMap{"PORT": "3011"},
		},
		{
			name:            "falls back to appending when the git line cannot be rewritten safely",
			gitContent:      "MULTI=\"line1\nline2\"\nOTHER=1\n",
			overrideContent: "MULTI=replaced\n",
			want:            "MULTI=\"line1\nline2\"\nOTHER=1\nMULTI=replaced\n",
			wantEnv:         EnvMap{"MULTI": "replaced", "OTHER": "1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effective, err := BuildEffectiveEnvContent(tt.gitContent, tt.overrideContent)
			require.NoError(t, err)
			assert.Equal(t, tt.want, effective)

			parsed, err := ParseProjectEnvContent(effective, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEnv, parsed)
		})
	}
}

func TestBuildOverrideEnvContent(t *testing.T) {
	t.Run("includes only values that differ from git", func(t *testing.T) {
		gitContent := "BASE_URL=https://example.com\nSHARED=git\n"
		effectiveContent := "BASE_URL=https://example.com\nSHARED=git\nAPI_TOKEN=secret\n"

		override, err := BuildOverrideEnvContent(gitContent, effectiveContent)
		require.NoError(t, err)
		assert.Equal(t, "API_TOKEN=secret\n", override)
	})

	t.Run("falls back to git for removed git variables", func(t *testing.T) {
		gitContent := "BASE_URL=https://example.com\nREMOVE_ME=1\n"
		effectiveContent := "BASE_URL=https://example.com\n"

		override, err := BuildOverrideEnvContent(gitContent, effectiveContent)
		require.NoError(t, err)
		assert.Empty(t, override)
	})

	t.Run("drops empty overrides for git-backed keys during normalization", func(t *testing.T) {
		gitContent := "BASE_URL=https://example.com\nREMOVE_ME=1\n"
		effectiveContent := "BASE_URL=https://example.com\nREMOVE_ME=\nTOKEN=local\n"

		override, err := BuildOverrideEnvContent(gitContent, effectiveContent)
		require.NoError(t, err)
		assert.Equal(t, "TOKEN=local\n", override)
	})

	t.Run("keeps explicit empty local-only values", func(t *testing.T) {
		gitContent := "BASE_URL=https://example.com\n"
		effectiveContent := "BASE_URL=https://example.com\nLOCAL_EMPTY=\n"

		override, err := BuildOverrideEnvContent(gitContent, effectiveContent)
		require.NoError(t, err)
		assert.Equal(t, "LOCAL_EMPTY=\n", override)
	})

	t.Run("override derivation keeps local-only keys during migration", func(t *testing.T) {
		gitContent := "BASE_URL=https://example.com\nREMOTE_ONLY=1\n"
		effectiveContent := "BASE_URL=https://example.com\nTOKEN=local\n"

		override, err := BuildOverrideEnvContent(gitContent, effectiveContent)
		require.NoError(t, err)
		assert.Equal(t, "TOKEN=local\n", override)
	})

	t.Run("additive migration keeps only local-only keys when a direct env becomes git-managed", func(t *testing.T) {
		gitContent := "TOKEN=git\nREMOTE_ONLY=1\n"
		localContent := "TOKEN=stale-local\nLOCAL_ONLY=1\n"

		override, err := BuildAdditiveOverrideEnvContent(gitContent, localContent)
		require.NoError(t, err)
		assert.Equal(t, "LOCAL_ONLY=1\n", override)
	})

	t.Run("preserves an existing valid override verbatim", func(t *testing.T) {
		gitContent := "BASE_URL=https://example.com\n"
		overrideContent := "# keep local formatting\nTOKEN='$pbkdf2-sha512$310000$XXX'\n"

		override, err := BuildOverrideEnvContent(gitContent, overrideContent)
		require.NoError(t, err)
		assert.Equal(t, overrideContent, override)
	})

	t.Run("generated overrides escape literal dollars", func(t *testing.T) {
		gitContent := "BASE_URL=https://example.com\n"
		effectiveContent := "BASE_URL=https://example.com\nTOKEN='$pbkdf2-sha512$310000$XXX'\n"

		override, err := BuildOverrideEnvContent(gitContent, effectiveContent)
		require.NoError(t, err)
		assert.Equal(t, "TOKEN=$$pbkdf2-sha512$$310000$$XXX\n", override)

		parsed, err := ParseProjectEnvContent(override, nil)
		require.NoError(t, err)
		assert.Equal(t, "$pbkdf2-sha512$310000$XXX", parsed["TOKEN"])
	})
}

func TestFormatEnvMapInternal_RoundTripsValues(t *testing.T) {
	values := []string{
		"$pbkdf2-sha512$310000$XXX",
		"prefix $VALUE with spaces",
		`quote " and apostrophe '`,
		`backslash\$VALUE`,
		"line one\nline two",
		"$$literal-dollars",
	}

	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			content := formatEnvMapInternal(EnvMap{"VALUE": value})
			parsed, err := ParseProjectEnvContent(content, nil)
			require.NoError(t, err)
			assert.Equal(t, value, parsed["VALUE"])
		})
	}
}

func TestReadProjectEnvState(t *testing.T) {
	t.Run("direct mode uses .env as editable source", func(t *testing.T) {
		projectDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, EffectiveEnvFileName), []byte("FOO=bar\n"), utils.FilePerm))

		state, err := ReadProjectEnvState(projectDir)
		require.NoError(t, err)
		assert.Equal(t, ProjectEnvModeDirect, state.Mode)
		assert.Equal(t, EffectiveEnvFileName, state.EditableFileName)
		assert.Equal(t, "FOO=bar\n", state.EditableContent)
		assert.False(t, state.HasGitSource)
		assert.False(t, state.HasOverride)
	})

	t.Run("override mode exposes project.env and git source separately", func(t *testing.T) {
		projectDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, EffectiveEnvFileName), []byte("A=1\nB=2\n"), utils.FilePerm))
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, GitSourceEnvFileName), []byte("A=1\n"), utils.FilePerm))
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, OverrideEnvFileName), []byte("B=2\n"), utils.FilePerm))

		state, err := ReadProjectEnvState(projectDir)
		require.NoError(t, err)
		assert.Equal(t, ProjectEnvModeOverride, state.Mode)
		assert.Equal(t, OverrideEnvFileName, state.EditableFileName)
		assert.Equal(t, "B=2\n", state.EditableContent)
		assert.True(t, state.HasGitSource)
		assert.Equal(t, "A=1\n", state.GitContent)
		assert.True(t, state.HasOverride)
		assert.Equal(t, "A=1\nB=2\n", state.EffectiveContent)
	})
}

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
