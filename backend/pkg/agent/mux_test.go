package agent

import (
	"testing"

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
