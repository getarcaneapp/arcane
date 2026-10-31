//go:build unix

package snapshots

import (
	"os"
	"syscall"
)

func preserveOwnership(path string, info os.FileInfo) error {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return os.Chown(path, int(stat.Uid), int(stat.Gid))
	}
	return nil
}
