package ws

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
)

func TestApplyCPUAffinityLimitInternal(t *testing.T) {
	affinity := runtime.NumCPU()

	// A host-wide count larger than the affinity mask (LXC without lxcfs,
	// #3161) is capped at the mask size; a smaller cgroup-derived count wins.
	require.Equal(t, affinity, applyCPUAffinityLimitInternal(affinity+8))
	require.Equal(t, affinity, applyCPUAffinityLimitInternal(0))
	if affinity > 1 {
		require.Equal(t, 1, applyCPUAffinityLimitInternal(1))
	}
}

func TestSystemStatsSamplerEffectiveInterval(t *testing.T) {
	tests := []struct {
		name      string
		intervals map[time.Duration]int
		want      time.Duration
	}{
		{
			name: "no subscribers falls back to the default",
			want: systemStatsSamplerDefaultInterval,
		},
		{
			// The sampler must not tick faster than anyone reads.
			name:      "single slow subscriber sets the pace",
			intervals: map[time.Duration]int{5 * time.Second: 1},
			want:      5 * time.Second,
		},
		{
			name:      "fastest of several subscribers wins",
			intervals: map[time.Duration]int{5 * time.Second: 1, 2 * time.Second: 1},
			want:      2 * time.Second,
		},
		{
			// Mirrors releasing the fast subscriber from the case above.
			name:      "remaining subscriber's pace after a release",
			intervals: map[time.Duration]int{5 * time.Second: 1},
			want:      5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &WebSocketHandler{}
			h.systemStatsSampler.intervals = tt.intervals
			h.refreshSystemStatsSamplerIntervalLockedInternal()
			require.Equal(t, tt.want, h.systemStatsSamplerIntervalInternal())
		})
	}
}

// TestSystemStatsSamplerRearmsOnFasterSubscriber verifies that a faster
// subscriber joining a running sampler re-arms the ticker immediately
// instead of waiting out the slower tick already in flight (which left
// the fast subscriber rereading a stale snapshot for up to the old
// interval).
func TestSystemStatsSamplerRearmsOnFasterSubscriber(t *testing.T) {
	collections := make(chan struct{}, 16)
	h := &WebSocketHandler{
		cpuUsageReader: func(time.Duration) (float64, bool) { return 0, true },
		systemStatsCollector: func(context.Context) systemtypes.SystemStats {
			select {
			case collections <- struct{}{}:
			default:
			}
			return systemtypes.SystemStats{}
		},
	}

	ctx := context.Background()
	slow := time.Hour
	require.True(t, h.acquireSystemStatsSamplerInternal(ctx, slow))
	defer h.releaseSystemStatsSamplerInternal(slow)

	// Drain the ready-gate collection; the ticker is now armed for an hour.
	select {
	case <-collections:
	case <-time.After(5 * time.Second):
		t.Fatal("sampler never took its initial snapshot")
	}

	fast := 10 * time.Millisecond
	require.True(t, h.acquireSystemStatsSamplerInternal(ctx, fast))
	defer h.releaseSystemStatsSamplerInternal(fast)

	select {
	case <-collections:
	case <-time.After(5 * time.Second):
		t.Fatal("sampler did not re-arm to the faster subscriber's interval")
	}
}
