package agent

import (
	"testing"

	"github.com/hashicorp/yamux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMuxConnCountsBytes(t *testing.T) {
	frames := make(chan []byte, 1)
	frames <- []byte("hello")
	conn := newMuxConn(
		func([]byte) error {
			return nil
		},
		func() ([]byte, error) {
			return <-frames, nil
		},
		func() error {
			return nil
		},
	)
	t.Cleanup(func() {
		_ = conn.Close()
	})

	written, err := conn.Write([]byte("abc"))
	require.NoError(t, err)
	assert.Equal(t, 3, written)

	buf := make([]byte, 3)
	read, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 3, read)
	read, err = conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 2, read)

	assert.Equal(t, int64(5), conn.rx.Load())
	assert.Equal(t, int64(3), conn.tx.Load())
}

func testStreamWindowMiB() int {
	return 16
}

func TestNewMuxConfigStreamWindow(t *testing.T) {
	tests := []struct {
		name            string
		streamWindowMiB int
		want            uint32
		keepAlive       bool
	}{
		{name: "agent minimum", keepAlive: true, streamWindowMiB: 1, want: 1 << 20},
		{name: "manager default", streamWindowMiB: 16, want: 16 << 20},
		{name: "agent maximum", keepAlive: true, streamWindowMiB: 64, want: 64 << 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newMuxConfig(tt.keepAlive, tt.streamWindowMiB)
			assert.Equal(t, tt.want, cfg.MaxStreamWindowSize)
			assert.Equal(t, tt.keepAlive, cfg.EnableKeepAlive)
			require.NoError(t, yamux.VerifyConfig(cfg))
		})
	}
}

func TestMuxStreamWindowMiB(t *testing.T) {
	tests := []struct {
		name         string
		capabilities []string
		fallback     int
		want         int
	}{
		{name: "older agent uses manager value", capabilities: []string{tunnelCapabilityMux}, fallback: 16, want: 16},
		{name: "advertised value wins", capabilities: []string{tunnelCapabilityMux, tunnelCapabilityMuxWindowPrefix + "48"}, fallback: 16, want: 48},
		{name: "clamped to minimum", capabilities: []string{tunnelCapabilityMuxWindowPrefix + "0"}, fallback: 16, want: 1},
		{name: "clamped to maximum", capabilities: []string{tunnelCapabilityMuxWindowPrefix + "4096"}, fallback: 16, want: 64},
		{name: "malformed value uses manager value", capabilities: []string{tunnelCapabilityMuxWindowPrefix + "lots"}, fallback: 24, want: 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, muxStreamWindowMiB(tt.capabilities, tt.fallback))
		})
	}
}
