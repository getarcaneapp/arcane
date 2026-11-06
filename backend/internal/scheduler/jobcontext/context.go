package jobcontext

import (
	"context"
	"slices"

	"getarcane.dev/types/v3"
)

type (
	progressKey struct{}
	runKey      struct{}
)

func WithExecution(ctx context.Context, run types.SchedulerRun, persist func(types.TargetOutcome) error) context.Context {
	ctx = context.WithValue(ctx, runKey{}, run)
	if persist != nil {
		ctx = context.WithValue(ctx, progressKey{}, persist)
	}
	return ctx
}

func Progress(ctx context.Context, target types.TargetOutcome) error {
	if persist, ok := ctx.Value(progressKey{}).(func(types.TargetOutcome) error); ok {
		return persist(target)
	}
	return nil
}

func Merge(targets []types.TargetOutcome, target types.TargetOutcome) []types.TargetOutcome {
	index := slices.IndexFunc(targets, func(existing types.TargetOutcome) bool { return existing.ID == target.ID })
	if index < 0 {
		return append(targets, target)
	}
	if len(target.RecoveryData) == 0 {
		target.RecoveryData = targets[index].RecoveryData
	}
	targets[index] = target
	return targets
}

func Run(ctx context.Context) (types.SchedulerRun, bool) {
	run, ok := ctx.Value(runKey{}).(types.SchedulerRun)
	return run, ok
}

func ConfirmedTarget(run types.SchedulerRun, targetID string) types.SchedulerOutcome {
	for _, target := range run.Outcome.Targets {
		if target.ID == targetID && target.Status == types.RunStatusSucceeded {
			return types.SchedulerOutcome{Status: types.RunStatusSucceeded, ActivityID: target.ActivityID, Targets: run.Outcome.Targets}
		}
	}
	return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: "The interrupted operation has no confirmed completion", Targets: run.Outcome.Targets}
}
