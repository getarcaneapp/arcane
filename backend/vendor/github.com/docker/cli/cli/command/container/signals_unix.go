//go:build !windows

package container

import (
	"os"

	"golang.org/x/sys/unix"
)

func isRuntimeSig(s os.Signal) bool {
	return s == unix.SIGURG
}
