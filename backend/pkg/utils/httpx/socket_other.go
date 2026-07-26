//go:build !linux

package httpx

import (
	"syscall"
	"time"
)

// setDeadPeerTimeoutInternal is a no-op outside Linux. TCP_USER_TIMEOUT has no
// portable equivalent, and Arcane only ships Linux images; local dev builds
// keep the OS default retransmission behaviour.
func setDeadPeerTimeoutInternal(_ syscall.RawConn, _ time.Duration) error {
	return nil
}
