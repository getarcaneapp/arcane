// Package jobcontext carries durable execution progress through domain operations.
package jobcontext

import (
	"context"
	"slices"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

type (
	progressKeyInternal struct{}
	runKeyInternal      struct{}
)

// WithExecution binds the run a job executes and, when persist is set, how its target progress is saved.
func WithExecution(ctx context.Context, run schedulertypes.Run, persist func(schedulertypes.TargetOutcome) error) context.Context {
	ctx = context.WithValue(ctx, runKeyInternal{}, run)
	if persist != nil {
		ctx = context.WithValue(ctx, progressKeyInternal{}, persist)
	}
	return ctx
}

// Progress saves target progress for the executing run; it is a no-op outside one.
func Progress(ctx context.Context, target schedulertypes.TargetOutcome) error {
	if persist, ok := ctx.Value(progressKeyInternal{}).(func(schedulertypes.TargetOutcome) error); ok {
		return persist(target)
	}
	return nil
}

// Merge records target progress in place of the entry with its ID, keeping recovery data the update omits.
func Merge(targets []schedulertypes.TargetOutcome, target schedulertypes.TargetOutcome) []schedulertypes.TargetOutcome {
	index := slices.IndexFunc(targets, func(existing schedulertypes.TargetOutcome) bool { return existing.ID == target.ID })
	if index < 0 {
		return append(targets, target)
	}
	if len(target.RecoveryData) == 0 {
		target.RecoveryData = targets[index].RecoveryData
	}
	targets[index] = target
	return targets
}

// Run returns the run being executed, as of when its execution started.
func Run(ctx context.Context) (schedulertypes.Run, bool) {
	run, ok := ctx.Value(runKeyInternal{}).(schedulertypes.Run)
	return run, ok
}

// ConfirmedTarget returns a terminal outcome only for a target whose completion
// was persisted or confirmed against its activity by the execution coordinator.
func ConfirmedTarget(run schedulertypes.Run, targetID string) schedulertypes.Outcome {
	for _, target := range run.Outcome.Targets {
		if target.ID == targetID && target.Status == schedulertypes.Succeeded {
			return schedulertypes.Outcome{Status: schedulertypes.Succeeded, ActivityID: target.ActivityID, Targets: run.Outcome.Targets}
		}
	}
	return schedulertypes.Outcome{Status: schedulertypes.Failed, Message: "The interrupted operation has no confirmed completion", Targets: run.Outcome.Targets}
}
