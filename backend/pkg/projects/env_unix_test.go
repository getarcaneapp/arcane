//go:build unix

package projects

import (
	stderrors "errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadProjectEnvState_TreatsPermissionLockedEnvAsUnreadable verifies that a
// chmod 000 .env (e.g. root-owned or foreign-owned) is reported as present but
// unreadable instead of failing the whole read, so callers can leave it
// untouched rather than aborting a git sync.
func TestReadProjectEnvState_TreatsPermissionLockedEnvAsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}

	projectDir := t.TempDir()
	envPath := filepath.Join(projectDir, EffectiveEnvFileName)
	require.NoError(t, os.WriteFile(envPath, []byte("FOO=locked\n"), utils.FilePerm))
	require.NoError(t, os.Chmod(envPath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(envPath, 0o644) })

	state, err := ReadProjectEnvState(projectDir)
	require.NoError(t, err)
	assert.True(t, state.EffectiveUnreadable)
	assert.False(t, state.HasEffective)
	assert.Empty(t, state.EffectiveContent)

	// The file itself is left untouched.
	require.NoError(t, os.Chmod(envPath, 0o644))
	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, "FOO=locked\n", string(content))
}

func TestReadProjectEnvState_TreatsDirectoryAsUnreadable(t *testing.T) {
	for _, fileName := range []string{EffectiveEnvFileName, GitSourceEnvFileName, OverrideEnvFileName} {
		t.Run(fileName, func(t *testing.T) {
			projectDir := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(projectDir, fileName), utils.DirPerm))

			state, err := ReadProjectEnvState(projectDir)
			require.NoError(t, err)
			assert.False(t, state.HasEffective)
			assert.False(t, state.HasGitSource)
			assert.False(t, state.HasOverride)
			assert.Equal(t, fileName == EffectiveEnvFileName, state.EffectiveUnreadable)
			assert.Equal(t, fileName == GitSourceEnvFileName, state.GitSourceUnreadable)
			assert.Equal(t, fileName == OverrideEnvFileName, state.OverrideUnreadable)
		})
	}
}

func TestParseEnvFile_IgnoresDirectory(t *testing.T) {
	envDir := filepath.Join(t.TempDir(), EffectiveEnvFileName)
	require.NoError(t, os.Mkdir(envDir, utils.DirPerm))

	projectEnv, err := ParseProjectEnvFile(envDir, EnvMap{})
	require.NoError(t, err)
	assert.Nil(t, projectEnv)

	validationEnv, err := ParseValidationEnvFile(envDir, EnvMap{})
	require.NoError(t, err)
	assert.Nil(t, validationEnv)
}

func TestWithTransientValidationEnvFile_LeavesEnvDirectoryUntouched(t *testing.T) {
	projectDir := t.TempDir()
	envDir := filepath.Join(projectDir, EffectiveEnvFileName)
	require.NoError(t, os.Mkdir(envDir, utils.DirPerm))
	nested := filepath.Join(envDir, "keep")
	require.NoError(t, os.WriteFile(nested, []byte("x"), utils.FilePerm))

	content := "FOO=bar\n"
	runErr := stderrors.New("validation failed")
	called := false
	err := WithTransientValidationEnvFile(t.Context(), projectDir, &content, func() error {
		called = true
		return runErr
	})
	require.ErrorIs(t, err, runErr)
	assert.True(t, called)

	info, err := os.Stat(envDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	_, err = os.Stat(nested)
	require.NoError(t, err)
}

func TestValidateComposeContentForUpdate_EnvDirectory(t *testing.T) {
	projectsDir := t.TempDir()
	projectDir := filepath.Join(projectsDir, "envdir")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, EffectiveEnvFileName), utils.DirPerm))

	compose := "services:\n  app:\n    image: nginx:alpine\n"
	content := "FOO=bar\n"
	require.NoError(t, ValidateComposeContentForUpdate(t.Context(), projectsDir, projectDir, "envdir", compose, &content, nil, "", false))
}
