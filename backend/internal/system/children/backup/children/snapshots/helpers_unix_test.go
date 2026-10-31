//go:build unix

package snapshots

import (
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func requireSameOwner(t *testing.T, expected, actual os.FileInfo) {
	t.Helper()
	require.Equal(t, expected.Sys().(*syscall.Stat_t).Uid, actual.Sys().(*syscall.Stat_t).Uid)
	require.Equal(t, expected.Sys().(*syscall.Stat_t).Gid, actual.Sys().(*syscall.Stat_t).Gid)
}
