//go:build !linux

package httpx

import (
	"syscall"
	"time"
)

//nolint:shimbad // build-tag stub: the real implementation lives in socket_linux.go
func setDeadPeerTimeout(_ syscall.RawConn, _ time.Duration) error {
	return nil
}
