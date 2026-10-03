package execution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/host/local"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/userctx"
)

// Dependencies are the updater operations single-container delivery runs on.
type Dependencies struct {
	Coordinator      *runs.Coordinator
	Config           *config.Config
	Roles            *role.RoleService
	Activity         *activity.ActivityService
	Logger           func() *slog.Logger
	AcquireUpdate    func(ctx context.Context) (context.Context, func(), error)
	UpdateBusy       error
	TrackActivity    func(ctx context.Context, activityID string) context.Context
	RunUpdate        func(ctx context.Context, containerID, activityID string) (*updater.Result, error)
	FinishUpdate     func(ctx context.Context, activityID string, result *updater.Result, runErr error)
	FreezeContainer  func(ctx context.Context, containerID string) (*updater.FrozenUpdateTarget, error)
	ConfirmTarget    func(ctx context.Context, target updater.FrozenUpdateTarget) (bool, bool, error)
	WithFrozenTarget func(ctx context.Context, target *updater.FrozenUpdateTarget, persist func() error) context.Context
}

// Service delivers accepted single-container updates through durable Francis
// jobs and repairs interrupted deliveries.
type Service struct {
	coordinator      *runs.Coordinator
	config           *config.Config
	roles            *role.RoleService
	activity         *activity.ActivityService
	logger           func() *slog.Logger
	acquireUpdate    func(ctx context.Context) (context.Context, func(), error)
	updateBusy       error
	trackActivity    func(ctx context.Context, activityID string) context.Context
	runUpdate        func(ctx context.Context, containerID, activityID string) (*updater.Result, error)
	finishUpdate     func(ctx context.Context, activityID string, result *updater.Result, runErr error)
	freezeContainer  func(ctx context.Context, containerID string) (*updater.FrozenUpdateTarget, error)
	confirmTarget    func(ctx context.Context, target updater.FrozenUpdateTarget) (bool, bool, error)
	withFrozenTarget func(ctx context.Context, target *updater.FrozenUpdateTarget, persist func() error) context.Context

	singleUpdates  *actor.Service
	singleMu       sync.Mutex
	singleContext  context.Context
	singleCancel   context.CancelFunc
	singleWorkers  sync.WaitGroup
	singleDone     chan struct{}
	singleStopping bool
}

func NewService(deps Dependencies) *Service {
	return &Service{
		coordinator:      deps.Coordinator,
		config:           deps.Config,
		roles:            deps.Roles,
		activity:         deps.Activity,
		logger:           deps.Logger,
		acquireUpdate:    deps.AcquireUpdate,
		updateBusy:       deps.UpdateBusy,
		trackActivity:    deps.TrackActivity,
		runUpdate:        deps.RunUpdate,
		finishUpdate:     deps.FinishUpdate,
		freezeContainer:  deps.FreezeContainer,
		confirmTarget:    deps.ConfirmTarget,
		withFrozenTarget: deps.WithFrozenTarget,
	}
}

// Ready reports whether the actor host has been registered.
func (s *Service) Ready() bool {
	return s.singleUpdates != nil
}

// Submit persists an accepted update intent and dispatches it without waiting
// for the update to run.
func (s *Service) Submit(ctx, workCtx context.Context, containerID, activityID string) error {
	command := singleUpdateCommandInternal(ctx, containerID, activityID)
	state := updater.SingleUpdateState{Command: command, Status: "queued"}
	if setStateErr := s.singleUpdates.SetState(workCtx, StateType, activityID, state, nil); setStateErr != nil {
		s.finishUpdate(workCtx, activityID, nil, setStateErr)
		return fmt.Errorf("persist container update: %w", setStateErr)
	}
	if dispatchSingleErr := s.dispatchSingleInternal(workCtx, state); dispatchSingleErr != nil {
		s.logger().WarnContext(workCtx, "container update dispatch deferred", "activityId", activityID, "error", dispatchSingleErr)
	}
	return nil
}

// ActorType and StateType are the durable Francis identifiers of
// single-container update jobs and their persisted delivery state.
const (
	ActorType = "single-container-update"
	StateType = "single-container-update-state"
)

type singleUpdateActorInternal struct{ service *Service }

func (s *Service) RegisterActors(runtime *francis.Runtime) error {
	s.singleUpdates = runtime.Service()
	return runtime.RegisterActor(
		ActorType,
		func(
			_ string,
			_ *actor.Service,
		) actor.Actor {
			return &singleUpdateActorInternal{
				service: s,
			}
		},
		local.WithCapacityGroup(
			"jobs",
			4,
		),
		local.WithCompletedJobRetention(
			7*24*time.Hour,
		),
	)
}

// Start repairs persisted single-container delivery intents until shutdown.
func (s *Service) Start(ctx context.Context) error {
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
					s.logger().WarnContext(s.singleContext, "repair container update dispatch", "error", err)
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
func (s *Service) Stop(ctx context.Context) error {
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

func (s *Service) dispatchSingleInternal(ctx context.Context, state updater.SingleUpdateState) error {
	if s.coordinator != nil && !s.coordinator.Active() {
		return nil
	}
	id, _, err := s.singleUpdates.Dispatch(ctx, ActorType, "single-container", "update", state.Command, actor.WithIdempotencyKey(state.Command.ActivityID))
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
	if deleteJobErr := s.singleUpdates.DeleteJob(ctx, ActorType, "single-container", id); deleteJobErr != nil {
		return deleteJobErr
	}
	_, _, err = s.singleUpdates.Dispatch(ctx, ActorType, "single-container", "update", state.Command, actor.WithIdempotencyKey(state.Command.ActivityID))
	return err
}

// repairSinglesInternal scans every persisted update, collecting per-entry
// failures so one bad entry cannot block repair of the rest.
func (s *Service) repairSinglesInternal(ctx context.Context) error {
	var failures []error
	for cursor := ""; ; {
		page, err := s.singleUpdates.ListStates(ctx, StateType, &actor.ListStatesOpts{IncludeData: true, After: cursor, Limit: 100})
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		for _, entry := range page.States {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return errors.Join(append(failures, ctxErr)...)
			}
			state, decodeErr := decodeSingleStateInternal(entry)
			if decodeErr == nil {
				decodeErr = s.repairSingleInternal(ctx, state)
			}
			if decodeErr != nil {
				failures = append(failures, fmt.Errorf("repair container update %s: %w", entry.ActorID, decodeErr))
			}
		}
		cursor = page.AfterID()
		if cursor == "" {
			return errors.Join(failures...)
		}
	}
}

func (s *Service) repairSingleInternal(ctx context.Context, state updater.SingleUpdateState) error {
	if state.Status == "queued" || state.Status == "running" {
		return s.dispatchSingleInternal(ctx, state)
	}
	if s.activity == nil {
		return nil
	}
	detail, err := s.activity.GetActivityDetail(ctx, "0", state.Command.ActivityID, 1)
	// Pruned or deleted history needs no projection repair.
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
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
	s.finishUpdate(ctx, state.Command.ActivityID, state.Result, outcomeErr)
	return nil
}

func decodeSingleStateInternal(entry actor.StateInfo) (updater.SingleUpdateState, error) {
	var state updater.SingleUpdateState
	if entry.Data == nil {
		return state, errors.New("empty update state")
	}
	err := entry.Data.Decode(&state)
	return state, err
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
	ctx, release, err := s.acquireUpdate(ctx)
	if err != nil {
		if errors.Is(err, s.updateBusy) {
			return actor.ErrJobRejected
		}
		return err
	}
	defer release()
	var command updater.SingleUpdateCommand
	if decodeErr := data.Decode(&command); decodeErr != nil {
		return errors.Join(decodeErr, actor.ErrJobPermanentFailure)
	}
	var state updater.SingleUpdateState
	if getStateErr := s.singleUpdates.GetState(ctx, StateType, command.ActivityID, &state); getStateErr != nil {
		return getStateErr
	}
	if state.Status == "completed" || state.Status == "needs_attention" {
		return nil
	}
	if state.Command != command {
		return actor.ErrJobPermanentFailure
	}
	if authorizeSingleErr := s.authorizeSingleInternal(ctx, command); authorizeSingleErr != nil {
		return s.failSingleInternal(ctx, &state, authorizeSingleErr.Error())
	}
	ctx = s.trackActivity(ctx, command.ActivityID)
	canceled, err := s.awaitSingleActivityInternal(ctx, lifetime, &state)
	if err != nil || canceled {
		return err
	}
	completed, err := s.prepareSingleTargetInternal(ctx, &state)
	if err != nil || completed {
		return err
	}
	ctx = s.withFrozenTarget(ctx, state.Target, func() error {
		return s.singleUpdates.SetState(ctx, StateType, command.ActivityID, state, nil)
	})
	result, runErr := func() (out *updater.Result, err error) {
		defer utils.RecoverToError(&err, "single container update")
		return s.runUpdate(ctx, state.Target.ContainerID, command.ActivityID)
	}()
	return s.completeSingleInternal(ctx, &state, result, runErr)
}

func (s *Service) failSingleInternal(ctx context.Context, state *updater.SingleUpdateState, reason string) error {
	state.Status = "needs_attention"
	state.Failure = reason
	if err := s.singleUpdates.SetState(ctx, StateType, state.Command.ActivityID, *state, nil); err != nil {
		return err
	}
	s.finishUpdate(ctx, state.Command.ActivityID, nil, errors.New(reason))
	return actor.ErrJobPermanentFailure
}

func (s *Service) authorizeSingleInternal(ctx context.Context, command updater.SingleUpdateCommand) error {
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

func (s *Service) awaitSingleActivityInternal(ctx, lifetime context.Context, state *updater.SingleUpdateState) (bool, error) {
	if s.activity == nil {
		return false, nil
	}
	detail, err := s.activity.GetActivityDetail(ctx, "0", state.Command.ActivityID, 1)
	if err != nil {
		return false, err
	}
	if detail.Activity.Status == activitytypes.StatusCancelled {
		state.Status = "needs_attention"
		state.Failure = "Container update cancelled"
		return true, s.singleUpdates.SetState(ctx, StateType, state.Command.ActivityID, *state, nil)
	}
	err = s.activity.AwaitActivitySlotBounded(ctx, state.Command.ActivityID, "0")
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
	return true, s.singleUpdates.SetState(writeCtx, StateType, state.Command.ActivityID, *state, nil)
}

func (s *Service) prepareSingleTargetInternal(ctx context.Context, state *updater.SingleUpdateState) (bool, error) {
	if state.Status != "running" {
		target, err := s.freezeContainer(ctx, state.Command.ContainerID)
		if err != nil {
			return false, s.failSingleInternal(ctx, state, err.Error())
		}
		state.Target = target
		state.Status = "running"
		return false, s.singleUpdates.SetState(ctx, StateType, state.Command.ActivityID, *state, nil)
	}
	if state.Target == nil {
		return false, s.failSingleInternal(ctx, state, "Interrupted container update has no saved target evidence")
	}
	confirmed, unchanged, err := s.confirmTarget(ctx, *state.Target)
	if err != nil {
		return false, err
	}
	if confirmed {
		result := &updater.Result{
			Updated: 1,
			Items: []updater.ResourceResult{
				{
					ResourceID:    state.Target.ContainerID,
					ResourceName:  state.Target.ContainerName,
					ResourceType:  "container",
					Status:        updater.StatusUpdated,
					UpdateApplied: true,
				},
			},
		}
		return true, s.completeSingleInternal(ctx, state, result, nil)
	}
	if !unchanged {
		return false, s.failSingleInternal(ctx, state, "Interrupted container update effect cannot be confirmed")
	}
	return false, nil
}

func (s *Service) completeSingleInternal(ctx context.Context, state *updater.SingleUpdateState, result *updater.Result, runErr error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if runErr != nil {
		return s.failSingleInternal(ctx, state, runErr.Error())
	}
	if state.Target.DesiredImageRef != "" && result != nil && result.Updated > 0 {
		confirmed, _, err := s.confirmTarget(ctx, *state.Target)
		if err != nil {
			return err
		}
		if !confirmed {
			return s.failSingleInternal(ctx, state, "Updater result does not confirm the frozen desired image")
		}
	}
	state.Status = "completed"
	state.Result = result
	if err := s.singleUpdates.SetState(ctx, StateType, state.Command.ActivityID, *state, nil); err != nil {
		return err
	}
	s.finishUpdate(ctx, state.Command.ActivityID, result, nil)
	return nil
}

func (s *Service) ActiveUpdateActivityIDs(ctx context.Context) ([]string, error) {
	ids := []string{}
	for cursor := ""; ; {
		page, err := s.singleUpdates.ListStates(ctx, StateType, &actor.ListStatesOpts{IncludeData: true, After: cursor, Limit: 100})
		if err != nil {
			return nil, err
		}
		for _, entry := range page.States {
			// States are keyed by activity ID, so an undecodable entry is still protected.
			if _, decodeErr := decodeSingleStateInternal(entry); decodeErr != nil {
				s.logger().WarnContext(ctx, "decode container update state", "activityID", entry.ActorID, "error", decodeErr)
			}
			ids = append(ids, entry.ActorID)
		}
		cursor = page.AfterID()
		if cursor == "" {
			return ids, nil
		}
	}
}

func singleUpdateCommandInternal(ctx context.Context, containerID, activityID string) updater.SingleUpdateCommand {
	command := updater.SingleUpdateCommand{ContainerID: containerID, ActivityID: activityID}
	if user, ok := userctx.CurrentUserFromContext(ctx); ok && user != nil {
		command.UserID = user.ID
	}
	command.KeyID, _ = ctx.Value(middleware.ContextKeyApiKeyID).(string)
	return command
}
