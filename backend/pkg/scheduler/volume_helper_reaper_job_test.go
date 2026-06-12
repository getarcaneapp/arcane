package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPruningVolumeHelperJob_Name(t *testing.T) {
	job := NewPruningVolumeHelperJob(nil, nil)
	require.Equal(t, PruningVolumeHelperJobName, job.Name())
}

func TestPruningVolumeHelperJob_Schedule(t *testing.T) {
	job := NewPruningVolumeHelperJob(nil, nil)
	// Fixed every-5-minute schedule; the idle timeout (read in Run) is the knob.
	require.Equal(t, "0 */5 * * * *", job.Schedule(context.Background()))
}

func TestPruningVolumeHelperJob_Reschedule(t *testing.T) {
	job := NewPruningVolumeHelperJob(nil, nil)
	require.NoError(t, job.Reschedule(context.Background()))
}

func TestPruningVolumeHelperJob_Run_NilVolumeServiceNoPanic(t *testing.T) {
	job := NewPruningVolumeHelperJob(nil, nil)
	// With no volume service the run must return without panicking (defensive guard).
	require.NotPanics(t, func() { job.Run(context.Background()) })
}
