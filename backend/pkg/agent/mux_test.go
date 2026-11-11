package agent

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kit.getarcane.app"
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

func TestMuxConnPendingLimitTracksDrainRate(t *testing.T) {
	tests := []struct {
		name      string
		sendDelay time.Duration
		payload   int
		wantLimit int64
		wantBatch int64
	}{
		{
			name:      "slow link keeps the queue small",
			sendDelay: 2 * muxPendingDrainTarget,
			payload:   3 * muxMinPendingWrite,
			wantLimit: muxMinPendingWrite,
			wantBatch: muxMinPendingWrite,
		},
		{
			name:      "fast link grows the queue",
			payload:   4 * muxMaxPendingWrite,
			wantLimit: muxMaxPendingWrite,
			wantBatch: muxMaxPendingWrite,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var largestBatch atomic.Int64
			conn := newMuxConn(
				func(p []byte) error {
					largestBatch.Store(max(largestBatch.Load(), int64(len(p))))
					time.Sleep(tt.sendDelay)
					return nil
				},
				func() ([]byte, error) {
					return nil, io.EOF
				},
				func() error {
					return nil
				},
			)
			t.Cleanup(func() {
				_ = conn.Close()
			})

			written, err := conn.Write(make([]byte, tt.payload))
			require.NoError(t, err)
			assert.Equal(t, tt.payload, written)
			require.Eventually(t, func() bool {
				return conn.pendingLimit.Load() == tt.wantLimit
			}, 5*time.Second, time.Millisecond)
			assert.LessOrEqual(t, largestBatch.Load(), tt.wantBatch)
		})
	}
}

func TestServeMuxSessionCompressesJSONUntilLinkIsFast(t *testing.T) {
	body := []byte(`{"items":["` + strings.Repeat("arcane", 4<<10) + `"]}`)
	server, tunnel := setupMockAgentServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", kit.Ternary(r.URL.RawQuery == "json", "application/json", "application/octet-stream"))
		if r.URL.RawQuery == "json" {
			_, _ = w.Write(body)
		} else {
			_, _ = w.Write(make([]byte, 2*muxPendingRateWindow))
		}
	})
	defer server.Close()
	defer func() { _ = tunnel.CloseWithReason("") }()

	get := func(path string) (*http.Response, []byte) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, MuxAgentBaseURL+path, http.NoBody)
		require.NoError(t, err)
		resp, err := tunnel.HTTPClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp, got
	}

	resp, got := get("/api/health?json")
	assert.True(t, resp.Uncompressed)
	assert.Equal(t, body, got)
	received, _ := tunnel.mux.traffic()
	assert.Less(t, received, int64(len(body)))

	resp, got = get("/api/health?bulk")
	assert.False(t, resp.Uncompressed)
	assert.Len(t, got, 2*muxPendingRateWindow)

	resp, got = get("/api/health?json")
	assert.False(t, resp.Uncompressed)
	assert.Equal(t, body, got)
}

func testStreamWindowMiB() int {
	return 16
}

func TestNewMuxConfigStreamWindow(t *testing.T) {
	tests := []struct {
		name            string
		streamWindowMiB int
		want            uint32
	}{
		{
			name:            "agent minimum",
			streamWindowMiB: 1,
			want:            1 << 20,
		},
		{
			name:            "manager default",
			streamWindowMiB: 16,
			want:            16 << 20,
		},
		{
			name:            "agent maximum",
			streamWindowMiB: 64,
			want:            64 << 20,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newMuxConfig(tt.streamWindowMiB)
			assert.Equal(t, tt.want, cfg.MaxStreamWindowSize)
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
		{
			name:         "older agent uses manager value",
			capabilities: []string{tunnelCapabilityMux},
			fallback:     16,
			want:         16,
		},
		{
			name:         "advertised value wins",
			capabilities: []string{tunnelCapabilityMux, tunnelCapabilityMuxWindowPrefix + "48"},
			fallback:     16,
			want:         48,
		},
		{
			name:         "clamped to minimum",
			capabilities: []string{tunnelCapabilityMuxWindowPrefix + "0"},
			fallback:     16,
			want:         1,
		},
		{
			name:         "clamped to maximum",
			capabilities: []string{tunnelCapabilityMuxWindowPrefix + "4096"},
			fallback:     16,
			want:         64,
		},
		{
			name:         "malformed value uses manager value",
			capabilities: []string{tunnelCapabilityMuxWindowPrefix + "lots"},
			fallback:     24,
			want:         24,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, muxStreamWindowMiB(tt.capabilities, tt.fallback))
		})
	}
}
