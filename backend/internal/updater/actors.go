package updater

import (
	"context"
	"errors"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	arcaneupdater "github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/host/local"
)

const (
	singleUpdateTypeInternal      = "single-container-update"
	singleUpdateStateTypeInternal = "single-container-update-state"
)

type singleUpdateActorInternal struct{ service *UpdaterService }

func (s *UpdaterService) RegisterActors(runtime *francis.Runtime) error {
	s.singleUpdates = runtime.Service()
	return runtime.RegisterActor(singleUpdateTypeInternal, func(_ string, _ *actor.Service) actor.Actor { return &singleUpdateActorInternal{service: s} }, local.WithCapacityGroup("jobs", 4), local.WithCompletedJobRetention(7*24*time.Hour))
}

// Start repairs persisted single-container delivery intents until shutdown.
func (s *UpdaterService) Start(ctx context.Context) error {
	s.singleMu.Lock()
	defer s.singleMu.Unlock()
	if s.singleContext != nil {
		return nil
	}
	s.singleContext, s.singleCancel = context.WithCancel(ctx)
	s.singleDone = make(chan struct{})
	go func() {
		defer close(s.singleDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			if s.coordinator == nil || s.coordinator.Active() {
				if err := s.repairSinglesInternal(s.singleContext); err != nil && s.singleContext.Err() == nil {
					s.loggerInternal().WarnContext(s.singleContext, "repair container update dispatch", "error", err)
				}
			}
			select {
			case <-s.singleContext.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

// Stop cancels business execution before the actor host drains jobs.
func (s *UpdaterService) Stop(ctx context.Context) error {
	s.singleMu.Lock()
	s.singleStopping = true
	if s.singleCancel != nil {
		s.singleCancel()
	}
	done := s.singleDone
	s.singleMu.Unlock()
	joined := make(chan struct{})
	go func() {
		if done != nil {
			<-done
		}
		s.singleWorkers.Wait()
		close(joined)
	}()
	select {
	case <-joined:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *UpdaterService) dispatchSingleInternal(ctx context.Context, state arcaneupdater.SingleUpdateState) error {
	if s.coordinator != nil && !s.coordinator.Active() {
		return nil
	}
	id, _, err := s.singleUpdates.Dispatch(ctx, singleUpdateTypeInternal, "single-container", "update", state.Command, actor.WithIdempotencyKey(state.Command.ActivityID))
	if err != nil {
		return err
	}
	job, err := s.singleUpdates.GetJob(ctx, id)
	if err != nil {
		return err
	}
	if !job.Status.IsTerminal() {
		return nil
	}
	// A terminal transport record cannot prove the durable business outcome.
	// Its replacement reconciles the saved effect before doing any further work.
	if err := s.singleUpdates.DeleteJob(ctx, singleUpdateTypeInternal, "single-container", id); err != nil {
		return err
	}
	_, _, err = s.singleUpdates.Dispatch(ctx, singleUpdateTypeInternal, "single-container", "update", state.Command, actor.WithIdempotencyKey(state.Command.ActivityID))
	return err
}

func (s *UpdaterService) repairSinglesInternal(ctx context.Context) error {
	for cursor := ""; ; {
		page, err := s.singleUpdates.ListStates(ctx, singleUpdateStateTypeInternal, &actor.ListStatesOpts{IncludeData: true, After: cursor, Limit: 100})
		if err != nil {
			return err
		}
		for _, entry := range page.States {
			var state arcaneupdater.SingleUpdateState
			if err := entry.Data.Decode(&state); err != nil {
				return err
			}
			if err := s.repairSingleInternal(ctx, state); err != nil {
				return err
			}
		}
		cursor = page.AfterID()
		if cursor == "" {
			return nil
		}
	}
}

func (s *UpdaterService) repairSingleInternal(ctx context.Context, state arcaneupdater.SingleUpdateState) error {
	if state.Status == "queued" || state.Status == "running" {
		return s.dispatchSingleInternal(ctx, state)
	}
	if s.deps.Activity == nil {
		return nil
	}
	detail, err := s.deps.Activity.GetActivityDetail(ctx, "0", state.Command.ActivityID, 1)
	if err != nil {
		return err
	}
	if detail.Activity.Status != activitytypes.StatusQueued && detail.Activity.Status != activitytypes.StatusRunning {
		return nil
	}
	var outcomeErr error
	if state.Failure != "" {
		outcomeErr = errors.New(state.Failure)
	}
	s.finishSingleContainerUpdateInternal(ctx, state.Command.ActivityID, state.Result, outcomeErr)
	return nil
}

func (a *singleUpdateActorInternal) Job(ctx context.Context, _ string, data actor.Envelope) error {
	s := a.service
	if s.coordinator != nil && !s.coordinator.Active() {
		return actor.ErrJobRejected
	}
	s.singleMu.Lock()
	if s.singleStopping {
		s.singleMu.Unlock()
		return actor.ErrJobRejected
	}
	s.singleWorkers.Add(1)
	lifetime := s.singleContext
	s.singleMu.Unlock()
	defer s.singleWorkers.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if lifetime != nil {
		//nolint:contextcheck // The service lifetime cancels the inherited Francis job context before host drain.
		stop := context.AfterFunc(lifetime, cancel)
		defer stop()
	}
	ctx, release, err := s.acquireUpdateInternal(ctx)
	if err != nil {
		if errors.Is(err, errUpdateBusyInternal) {
			return actor.ErrJobRejected
		}
		return err
	}
	defer release()
	var command arcaneupdater.SingleUpdateCommand
	if err := data.Decode(&command); err != nil {
		return errors.Join(err, actor.ErrJobPermanentFailure)
	}
	var state arcaneupdater.SingleUpdateState
	if err := s.singleUpdates.GetState(ctx, singleUpdateStateTypeInternal, command.ActivityID, &state); err != nil {
		return err
	}
	if state.Status == "completed" || state.Status == "needs_attention" {
		return nil
	}
	if state.Command != command {
		return actor.ErrJobPermanentFailure
	}
	if err := s.authorizeSingleInternal(ctx, command); err != nil {
		return s.failSingleInternal(ctx, &state, err.Error())
	}
	ctx = s.trackActivityInternal(ctx, command.ActivityID)
	canceled, err := s.awaitSingleActivityInternal(ctx, lifetime, &state)
	if err != nil || canceled {
		return err
	}
	completed, err := s.prepareSingleTargetInternal(ctx, &state)
	if err != nil || completed {
		return err
	}
	ctx = context.WithValue(ctx, frozenSingleKeyInternal{}, &frozenSingleInternal{Target: state.Target, Persist: func() error {
		return s.singleUpdates.SetState(ctx, singleUpdateStateTypeInternal, command.ActivityID, state, nil)
	}})
	result, runErr := func() (out *arcaneupdater.Result, err error) {
		defer utils.RecoverToError(&err, "single container update")
		return s.runSingleContainerUpdateInternal(ctx, state.Target.ContainerID, command.ActivityID)
	}()
	return s.completeSingleInternal(ctx, &state, result, runErr)
}

func (s *UpdaterService) failSingleInternal(ctx context.Context, state *arcaneupdater.SingleUpdateState, reason string) error {
	state.Status = "needs_attention"
	state.Failure = reason
	if err := s.singleUpdates.SetState(ctx, singleUpdateStateTypeInternal, state.Command.ActivityID, *state, nil); err != nil {
		return err
	}
	s.finishSingleContainerUpdateInternal(ctx, state.Command.ActivityID, nil, errors.New(reason))
	return actor.ErrJobPermanentFailure
}

func (s *UpdaterService) authorizeSingleInternal(ctx context.Context, command arcaneupdater.SingleUpdateCommand) error {
	if command.UserID == "" {
		return nil
	}
	if s.config != nil && s.config.AgentMode && command.UserID == "agent" && command.KeyID == "" {
		return nil
	}
	if s.roles == nil {
		return errors.New("requesting user permissions unavailable")
	}
	permissions, err := s.roles.ResolveExecutionPermissions(ctx, command.UserID, command.KeyID)
	if err != nil || !permissions.Allows(authz.PermImageUpdatesCheck, "0") {
		return errors.New("requesting user no longer has permission to update containers")
	}
	return nil
}

func (s *UpdaterService) awaitSingleActivityInternal(ctx, lifetime context.Context, state *arcaneupdater.SingleUpdateState) (bool, error) {
	if s.deps.Activity == nil {
		return false, nil
	}
	detail, err := s.deps.Activity.GetActivityDetail(ctx, "0", state.Command.ActivityID, 1)
	if err != nil {
		return false, err
	}
	if detail.Activity.Status == activitytypes.StatusCancelled {
		state.Status = "needs_attention"
		state.Failure = "Container update cancelled"
		return true, s.singleUpdates.SetState(ctx, singleUpdateStateTypeInternal, state.Command.ActivityID, *state, nil)
	}
	err = s.deps.Activity.AwaitActivitySlotBounded(ctx, state.Command.ActivityID, "0")
	if err == nil {
		return false, nil
	}
	if ctx.Err() == nil {
		return false, s.failSingleInternal(ctx, state, err.Error())
	}
	if lifetime != nil && lifetime.Err() != nil {
		return false, ctx.Err()
	}
	state.Status = "needs_attention"
	state.Failure = err.Error()
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return true, s.singleUpdates.SetState(writeCtx, singleUpdateStateTypeInternal, state.Command.ActivityID, *state, nil)
}

func (s *UpdaterService) prepareSingleTargetInternal(ctx context.Context, state *arcaneupdater.SingleUpdateState) (bool, error) {
	if state.Status != "running" {
		target, err := s.freezeContainerInternal(ctx, state.Command.ContainerID)
		if err != nil {
			return false, s.failSingleInternal(ctx, state, err.Error())
		}
		state.Target = target
		state.Status = "running"
		return false, s.singleUpdates.SetState(ctx, singleUpdateStateTypeInternal, state.Command.ActivityID, *state, nil)
	}
	if state.Target == nil {
		return false, s.failSingleInternal(ctx, state, "Interrupted container update has no saved target evidence")
	}
	confirmed, unchanged, err := s.confirmFrozenTargetInternal(ctx, *state.Target)
	if err != nil {
		return false, err
	}
	if confirmed {
		result := &arcaneupdater.Result{Updated: 1, Items: []arcaneupdater.ResourceResult{{ResourceID: state.Target.ContainerID, ResourceName: state.Target.ContainerName, ResourceType: "container", Status: arcaneupdater.StatusUpdated, UpdateApplied: true}}}
		return true, s.completeSingleInternal(ctx, state, result, nil)
	}
	if !unchanged {
		return false, s.failSingleInternal(ctx, state, "Interrupted container update effect cannot be confirmed")
	}
	return false, nil
}

func (s *UpdaterService) completeSingleInternal(ctx context.Context, state *arcaneupdater.SingleUpdateState, result *arcaneupdater.Result, runErr error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if runErr != nil {
		return s.failSingleInternal(ctx, state, runErr.Error())
	}
	if state.Target.DesiredImageRef != "" && result != nil && result.Updated > 0 {
		confirmed, _, err := s.confirmFrozenTargetInternal(ctx, *state.Target)
		if err != nil {
			return err
		}
		if !confirmed {
			return s.failSingleInternal(ctx, state, "Updater result does not confirm the frozen desired image")
		}
	}
	state.Status = "completed"
	state.Result = result
	if err := s.singleUpdates.SetState(ctx, singleUpdateStateTypeInternal, state.Command.ActivityID, *state, nil); err != nil {
		return err
	}
	s.finishSingleContainerUpdateInternal(ctx, state.Command.ActivityID, result, nil)
	return nil
}

func singleUpdateCommandInternal(ctx context.Context, containerID, activityID string) arcaneupdater.SingleUpdateCommand {
	command := arcaneupdater.SingleUpdateCommand{ContainerID: containerID, ActivityID: activityID}
	if user, ok := common.CurrentUserFromContext(ctx); ok && user != nil {
		command.UserID = user.ID
	}
	command.KeyID, _ = ctx.Value(middleware.ContextKeyApiKeyID).(string)
	return command
}

func (s *UpdaterService) ActiveUpdateActivityIDs(ctx context.Context) ([]string, error) {
	ids := []string{}
	for cursor := ""; ; {
		page, err := s.singleUpdates.ListStates(ctx, singleUpdateStateTypeInternal, &actor.ListStatesOpts{IncludeData: true, After: cursor, Limit: 100})
		if err != nil {
			return nil, err
		}
		for _, entry := range page.States {
			var state arcaneupdater.SingleUpdateState
			if err := entry.Data.Decode(&state); err != nil {
				return nil, err
			}
			ids = append(ids, state.Command.ActivityID)
		}
		cursor = page.AfterID()
		if cursor == "" {
			return ids, nil
		}
	}
}
