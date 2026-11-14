//go:build unix

package snapshots

import (
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireSameOwner asserts the staged file kept the source file's owner. The
// owner is a Unix concept (syscall.Stat_t), so the check is a no-op elsewhere.
func requireSameOwner(t *testing.T, original, staged os.FileInfo) {
	t.Helper()
	require.Equal(t, original.Sys().(*syscall.Stat_t).Uid, staged.Sys().(*syscall.Stat_t).Uid)
	require.Equal(t, original.Sys().(*syscall.Stat_t).Gid, staged.Sys().(*syscall.Stat_t).Gid)
}
