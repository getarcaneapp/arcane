package remoteutil

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

func DialContext(ctx context.Context, network string, addr string) (net.Conn, error) {
	var conn net.Conn
	var err error

	// dial context doesn't support named pipes
	if network == "npipe" {
		conn, err = winio.DialPipeContext(ctx, addr)
	} else {
		dialer := &net.Dialer{}
		conn, err = dialer.DialContext(ctx, network, addr)
	}

	return conn, err
}
