package scheduler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewEnvironmentHealthJob_Defaults(t *testing.T) {
	job := NewEnvironmentHealthJob(nil, nil)

	require.Equal(t, defaultEnvironmentSyncConcurrency, job.syncConcurrency)
	require.Equal(t, defaultEnvironmentSyncTimeout, job.syncTimeout)
	require.False(t, job.running.Load())
}

func TestEnvironmentHealthJob_RunGuardAtomic(t *testing.T) {
	job := &EnvironmentHealthJob{}

	require.True(t, job.running.CompareAndSwap(false, true))
	require.False(t, job.running.CompareAndSwap(false, true))

	job.running.Store(false)
	require.True(t, job.running.CompareAndSwap(false, true))
}
