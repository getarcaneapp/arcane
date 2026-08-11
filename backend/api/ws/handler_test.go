package ws

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
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
