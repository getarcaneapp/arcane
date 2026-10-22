package execution

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/workflow"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/userctx"
)

type (
	// Dependencies are the updater operations a single-container update runs on.
	Dependencies struct {
		Config        *config.Config
		Roles         *role.RoleService
		AcquireUpdate func(ctx context.Context, ids []string) (context.Context, func(), error)
		UpdateBusy    error
		RunUpdate     func(ctx context.Context, containerID, activityID string) (*types.UpdaterResult, error)
		// RecordUpdate saves the result on the activity and summarizes it as the run outcome.
		RecordUpdate     func(ctx context.Context, activityID string, result *types.UpdaterResult, runErr error) types.SchedulerOutcome
		FreezeContainer  func(ctx context.Context, containerID string) (*types.FrozenUpdateTarget, error)
		ConfirmTarget    func(ctx context.Context, target types.FrozenUpdateTarget) (bool, bool, error)
		ResumeTarget     func(ctx context.Context, target types.FrozenUpdateTarget) (types.FrozenUpdateTarget, bool, error)
		WithFrozenTarget func(ctx context.Context, target *types.FrozenUpdateTarget, persist func() error) context.Context
	}

	// Service runs accepted single-container updates as container-update workflows.
	Service struct {
		deps     Dependencies
		flow     *flow.Engine
		workflow *flow.Workflow
	}

	// updateInput is an accepted update: the container and who asked for it.
	updateInput struct {
		ContainerID string `json:"containerId"`
		UserID      string `json:"userId,omitempty"`
		KeyID       string `json:"keyId,omitempty"`
	}
)

func NewService(deps Dependencies) *Service {
	return &Service{deps: deps}
}

// RegisterWorkflows defines the container-update workflow while the host is still unstarted.
func (s *Service) RegisterWorkflows(engine *flow.Engine) error {
	defined, err := engine.Define(flow.Definition{
		Name:        "container-update",
		Version:     1,
		Fingerprint: "1b1bad80429a7061e5541b6fa57eb43b187046c3987bec83ea39f0496589ccf8",
		Concurrency: 4,
		Timeout:     2 * time.Hour,
		Activity: activitylib.StartRequest{
			Type:          types.ActivityTypeAutoUpdate,
			EnvironmentID: "0",
			Queue:         true,
			DeferSlot:     true,
			ResourceType:  new("container"),
			Step:          "Updating container",
			LatestMessage: "Container update started",
		},
		Labels: map[string]string{"update": "Updating container"},
		Steps:  []workflow.StepSpec{workflow.Step("update", engine.Handler(s.update), workflow.WithMaxAttempts(1))},
	})
	*s = Service{ //exhaustruct:enforce
		deps:     s.deps,
		flow:     engine,
		workflow: defined,
	}
	return err
}

// Submit accepts an update of the container and returns its activity ID; activity describes that activity.
func (s *Service) Submit(ctx context.Context, containerID string, activity activitylib.StartRequest) (string, error) {
	user, _ := userctx.CurrentUserFromContext(ctx)
	return s.flow.Submit(ctx, s.workflow, updateInput{
		ContainerID: containerID,
		UserID:      kit.FromPtr(user).ID,
		KeyID:       kit.As(ctx.Value(middleware.ContextKeyApiKeyID), ""),
	}, activity)
}

// update is the container-update step. A delivery that finds the target it froze confirms that target's effect
// before touching Docker again; while another update holds the updater, the task is handed back.
func (s *Service) update(ctx context.Context, t flow.Task) (any, error) {
	var input updateInput
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
	ctx, release, err := s.deps.AcquireUpdate(ctx, []string{input.ContainerID})
	if errors.Is(err, s.deps.UpdateBusy) {
		return nil, actor.ErrJobRejected
	}
	if err != nil {
		return nil, err
	}
	defer release()
	activityID := t.ActivityID()
	failed := func(reason string) types.SchedulerOutcome {
		return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: reason}
	}
	if input.UserID != "" && (!s.deps.Config.AgentMode || input.UserID != "agent" || input.KeyID != "") {
		permissions, permissionsErr := s.deps.Roles.ResolveExecutionPermissions(ctx, input.UserID, input.KeyID)
		if permissionsErr != nil || !permissions.Allows(authz.PermImageUpdatesCheck, "0") {
			return failed("requesting user no longer has permission to update containers"), nil
		}
	}

	var target types.FrozenUpdateTarget
	previous, _ := jobcontext.Run(ctx)
	if index := slices.IndexFunc(previous.Outcome.Targets, func(evidence types.TargetOutcome) bool {
		return evidence.ID == input.ContainerID && len(evidence.RecoveryData) > 0
	}); index >= 0 {
		if decodeErr := json.Unmarshal(previous.Outcome.Targets[index].RecoveryData, &target); decodeErr != nil {
			return nil, decodeErr
		}
		confirmed, unchanged, confirmErr := s.deps.ConfirmTarget(ctx, target)
		switch {
		case confirmErr != nil:
			return nil, confirmErr
		case confirmed:
			return s.deps.RecordUpdate(ctx, activityID, &types.UpdaterResult{
				Updated: 1,
				Items: []types.UpdaterResourceResult{{
					ResourceID:    target.ContainerID,
					ResourceName:  target.ContainerName,
					ResourceType:  "container",
					Status:        types.UpdaterStatusUpdated,
					UpdateApplied: true,
				}},
			}, nil), nil
		case !unchanged:
			return failed(fmt.Sprintf(
				"Interrupted container update effect cannot be confirmed: recreate %s from %s (previous image %s)",
				target.ContainerName,
				target.DesiredImageRef,
				target.BaselineImageID,
			)), nil
		}
		resumed, _, resumeErr := s.deps.ResumeTarget(ctx, target)
		if resumeErr != nil {
			return nil, resumeErr
		}
		target = resumed
	} else {
		frozen, freezeErr := s.deps.FreezeContainer(ctx, input.ContainerID)
		if freezeErr != nil {
			return failed(freezeErr.Error()), nil
		}
		target = *frozen
	}

	// The frozen target is the evidence a redelivery confirms, so it is recorded before and as the updater pins its image.
	record := func() error {
		data, marshalErr := json.Marshal(target)
		if marshalErr != nil {
			return marshalErr
		}
		return jobcontext.Progress(ctx, types.TargetOutcome{
			ID:           input.ContainerID,
			ResourceType: "container",
			Status:       types.RunStatusRunning,
			RecoveryData: data,
			ActivityID:   activityID,
		})
	}
	if recordErr := record(); recordErr != nil {
		return nil, recordErr
	}
	ctx = s.deps.WithFrozenTarget(ctx, &target, record)
	result, runErr := func() (out *types.UpdaterResult, err error) {
		defer utils.RecoverToError(&err, "single container update")
		return s.deps.RunUpdate(ctx, target.ContainerID, activityID)
	}()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if runErr == nil && target.DesiredImageRef != "" && result != nil && result.Updated > 0 {
		confirmed, _, confirmErr := s.deps.ConfirmTarget(ctx, target)
		if confirmErr != nil {
			return nil, confirmErr
		}
		if !confirmed {
			return failed("Updater result does not confirm the frozen desired image"), nil
		}
	}
	return s.deps.RecordUpdate(ctx, activityID, result, runErr), nil
}
