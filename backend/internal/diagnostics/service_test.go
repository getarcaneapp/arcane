package diagnostics

import (
	"context"
	"runtime/pprof"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticsServiceScanGoroutineLeaks(t *testing.T) {
	s := NewDiagnosticsService(nil, nil, nil)

	report, err := s.ScanGoroutineLeaks()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, report.Count, 0)
	assert.False(t, report.ScannedAt.IsZero())
	assert.Contains(t, report.Profile, "goroutineleak profile")

	rt, _, _ := s.Collect()
	assert.Equal(t, report.Count, rt.LeakedGoroutines)
	assert.False(t, rt.LeakScannedAt.IsZero())
	assert.True(t, rt.LeakScannedAt.Equal(report.ScannedAt))
}

func TestDiagnosticsServiceScanGoroutineLeaksReturnsCachedReport(t *testing.T) {
	s := NewDiagnosticsService(nil, nil, nil)

	first, err := s.ScanGoroutineLeaks()
	require.NoError(t, err)
	second, err := s.ScanGoroutineLeaks()
	require.NoError(t, err)
	assert.True(t, second.ScannedAt.Equal(first.ScannedAt))
	assert.Equal(t, first.Profile, second.Profile)
}

func TestDiagnosticsServiceGoroutines(t *testing.T) {
	s := NewDiagnosticsService(nil, nil, nil)
	release := make(chan struct{})
	defer close(release)
	// Different pprof labels split debug=1 records; the service merges them by stack.
	for _, worker := range []string{"a", "a", "b"} {
		go func() {
			pprof.Do(t.Context(), pprof.Labels("worker", worker), func(context.Context) { <-release })
		}()
	}

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		profile, err := s.Goroutines()
		require.NoError(c, err)
		assert.True(c, slices.IsSortedFunc(profile.Groups, func(a, b types.GoroutineGroup) int { return b.Count - a.Count }))
		assert.Equal(c, profile.Total, func() int {
			total := 0
			for _, g := range profile.Groups {
				total += g.Count
			}
			return total
		}())
		// The label is the outermost frame, the started func; the blocking closure and pprof.Do sit inside it.
		i := slices.IndexFunc(profile.Groups, func(g types.GoroutineGroup) bool {
			return strings.HasSuffix(g.Label, "TestDiagnosticsServiceGoroutines.func1")
		})
		require.GreaterOrEqual(c, i, 0)
		group := profile.Groups[i]
		assert.Equal(c, 3, group.Count)
		require.Len(c, group.Frames, 3)
		assert.Equal(c, "runtime/pprof.Do", group.Frames[1].Function)
		assert.False(c, slices.ContainsFunc(group.Frames, func(f types.GoroutineFrame) bool { return f.File == "" || f.Line <= 0 }))
	}, 5*time.Second, 20*time.Millisecond)
}
