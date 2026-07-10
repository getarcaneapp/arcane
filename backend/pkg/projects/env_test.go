package projects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEnvironment(t *testing.T) {
	// Setup temp dirs
	tmpDir := t.TempDir()
	projectsDir := filepath.Join(tmpDir, "projects")
	workdir := filepath.Join(projectsDir, "myproject")

	err := os.MkdirAll(workdir, pkgutils.DirPerm)
	require.NoError(t, err)

	// Create .env.global
	globalEnvContent := "GLOBAL_VAR=global_value\nSHARED_VAR=global_shared"
	err = os.WriteFile(filepath.Join(projectsDir, ".env.global"), []byte(globalEnvContent), pkgutils.FilePerm)
	require.NoError(t, err)

	// Create .env
	projectEnvContent := "PROJECT_VAR=project_value\nSHARED_VAR=project_shared"
	err = os.WriteFile(filepath.Join(workdir, ".env"), []byte(projectEnvContent), pkgutils.FilePerm)
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

	require.NoError(t, os.MkdirAll(workdir, pkgutils.DirPerm))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, ".env"), []byte("PROJECT_VAR=project_value\n"), pkgutils.FilePerm))

	loader := NewEnvLoader(projectsDir, workdir, false)
	ctx := context.Background()

	envMap, injectionVars, err := loader.LoadEnvironment(ctx)
	require.NoError(t, err)

	assert.Equal(t, "project_value", envMap["PROJECT_VAR"])
	assert.Empty(t, injectionVars)

	_, statErr := os.Stat(filepath.Join(projectsDir, GlobalEnvFileName))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
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
			name:            "adds a separator without changing either layer",
			gitContent:      "BASE_URL=https://example.com\nSHARED=git",
			overrideContent: "# local values\nAPI_TOKEN=secret\nSHARED=override\n",
			want:            "BASE_URL=https://example.com\nSHARED=git\n# local values\nAPI_TOKEN=secret\nSHARED=override\n",
			wantEnv: EnvMap{
				"BASE_URL":  "https://example.com",
				"API_TOKEN": "secret",
				"SHARED":    "override",
			},
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
		assert.Equal(t, "", override)
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
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, EffectiveEnvFileName), []byte("FOO=bar\n"), pkgutils.FilePerm))

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
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, EffectiveEnvFileName), []byte("A=1\nB=2\n"), pkgutils.FilePerm))
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, GitSourceEnvFileName), []byte("A=1\n"), pkgutils.FilePerm))
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, OverrideEnvFileName), []byte("B=2\n"), pkgutils.FilePerm))

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
