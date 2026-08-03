//go:build linux

package httpx

import (
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func setDeadPeerTimeoutInternal(rawConn syscall.RawConn, timeout time.Duration) error {
	var setErr error
	if err := rawConn.Control(func(fd uintptr) {
		setErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT, int(timeout.Milliseconds()))
	}); err != nil {
		return err
	}
	return setErr
}
