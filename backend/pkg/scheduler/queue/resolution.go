package queue

import (
	"context"
	"time"

	"emperror.dev/errors"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

// Resolve releases reviewed, inactive work without discarding its execution evidence.
func (q *Queue) Resolve(ctx context.Context, environmentID, jobID, runID, resolvedBy string) (st.Run, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.active[queueKeyInternal(environmentID, jobID)] {
		return st.Run{}, errors.New("job is still active")
	}
	run, err := q.Get(ctx, environmentID, jobID, runID)
	if err != nil {
		return run, err
	}
	err = q.UpdateRun(ctx, run, func(current *st.Run) error {
		if current.Status == st.Canceled && current.Resolution != nil {
			run = *current
			return nil
		}
		if current.Status != st.NeedsAttention {
			return errors.New("only runs needing attention can be resolved")
		}
		if current.EnvironmentID != "0" && (current.RemoteDeliveryAttempted || current.RemoteAccepted) && !current.RemoteSettled {
			return errors.New("remote execution must be confirmed and acknowledged before resolution")
		}
		now := time.Now().UTC()
		current.Status = st.Canceled
		current.Resolution = &st.RunResolution{ResolvedBy: resolvedBy, ResolvedAt: now, Reason: "resolved_after_review"}
		current.UpdatedAt = now
		current.FinishedAt = &now
		current.NextAttempt = nil
		current.Owner = ""
		run = *current
		return nil
	})
	if err == nil {
		q.signalInternal()
	}
	return run, err
}
