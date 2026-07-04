//go:build unix

package projects

import (
	"os"
	"path/filepath"
	"testing"

	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
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
	require.NoError(t, os.WriteFile(envPath, []byte("FOO=locked\n"), pkgutils.FilePerm))
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
