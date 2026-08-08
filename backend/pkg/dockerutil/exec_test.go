package docker

import (
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsExpectedStreamEndError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "eof", err: io.EOF, want: true},
		{name: "wrapped eof", err: fmt.Errorf("wrapped: %w", io.EOF), want: true},
		{name: "net err closed", err: net.ErrClosed, want: true},
		{name: "closed network connection", err: errors.New("read tcp 127.0.0.1:1234->127.0.0.1:5678: use of closed network connection"), want: true},
		{name: "broken pipe", err: errors.New("write: broken pipe"), want: true},
		{name: "connection reset by peer", err: errors.New("read: connection reset by peer"), want: true},
		{name: "unexpected", err: errors.New("some other error"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsExpectedStreamEndError(tt.err))
		})
	}
}
