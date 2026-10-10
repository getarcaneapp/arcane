package execution

import (
	"context"
	"encoding/json/v2"
	"errors"
	"slices"
	"time"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/builtin/workflow"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/userctx"
)

type (
	// Dependencies are the updater operations a single-container update runs on.
	Dependencies struct {
		Config        *config.Config
		Roles         *role.RoleService
		AcquireUpdate func(ctx context.Context) (context.Context, func(), error)
		UpdateBusy    error
		RunUpdate     func(ctx context.Context, containerID, activityID string) (*updater.Result, error)
		// RecordUpdate saves the result on the activity and summarizes it as the run outcome.
		RecordUpdate     func(ctx context.Context, activityID string, result *updater.Result, runErr error) scheduler.Outcome
		FreezeContainer  func(ctx context.Context, containerID string) (*updater.FrozenUpdateTarget, error)
		ConfirmTarget    func(ctx context.Context, target updater.FrozenUpdateTarget) (bool, bool, error)
		WithFrozenTarget func(ctx context.Context, target *updater.FrozenUpdateTarget, persist func() error) context.Context
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
	var err error
	s.flow = engine
	s.workflow, err = engine.Define(flow.Definition{
		Name:        "container-update",
		Version:     1,
		Fingerprint: "1b1bad80429a7061e5541b6fa57eb43b187046c3987bec83ea39f0496589ccf8",
		Concurrency: 4,
		Timeout:     2 * time.Hour,
		Activity: activitylib.StartRequest{
			Type: activitytypes.TypeAutoUpdate, EnvironmentID: "0", Queue: true, DeferSlot: true,
			ResourceType: new("container"), Step: "Updating container", LatestMessage: "Container update started",
		},
		Labels: map[string]string{"update": "Updating container"},
		Steps:  []workflow.StepSpec{workflow.Step("update", engine.Handler(s.update), workflow.WithMaxAttempts(1))},
	})
	return err
}

// Submit accepts an update of the container and returns its activity ID; activity describes that activity.
func (s *Service) Submit(ctx context.Context, containerID string, activity activitylib.StartRequest) (string, error) {
	input := updateInput{ContainerID: containerID}
	if user, ok := userctx.CurrentUserFromContext(ctx); ok && user != nil {
		input.UserID = user.ID
	}
	input.KeyID, _ = ctx.Value(middleware.ContextKeyApiKeyID).(string)
	return s.flow.Submit(ctx, s.workflow, input, activity)
}

// update is the container-update step. A delivery that finds the target it froze confirms that target's effect
// before touching Docker again; while another update holds the updater, the task is handed back.
func (s *Service) update(ctx context.Context, t flow.Task) (any, error) {
	var input updateInput
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
	ctx, release, err := s.deps.AcquireUpdate(ctx)
	if errors.Is(err, s.deps.UpdateBusy) {
		return nil, actor.ErrJobRejected
	}
	if err != nil {
		return nil, err
	}
	defer release()
	activityID := t.ActivityID()
	failed := func(reason string) scheduler.Outcome {
		return scheduler.Outcome{Status: scheduler.Failed, Message: reason}
	}
	if input.UserID != "" && (s.deps.Config == nil || !s.deps.Config.AgentMode || input.UserID != "agent" || input.KeyID != "") {
		if s.deps.Roles == nil {
			return failed("requesting user permissions unavailable"), nil
		}
		permissions, permissionsErr := s.deps.Roles.ResolveExecutionPermissions(ctx, input.UserID, input.KeyID)
		if permissionsErr != nil || !permissions.Allows(authz.PermImageUpdatesCheck, "0") {
			return failed("requesting user no longer has permission to update containers"), nil
		}
	}

	var target updater.FrozenUpdateTarget
	previous, _ := jobcontext.Run(ctx)
	if index := slices.IndexFunc(previous.Outcome.Targets, func(evidence scheduler.TargetOutcome) bool {
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
			item := updater.ResourceResult{
				ResourceID: target.ContainerID, ResourceName: target.ContainerName, ResourceType: "container",
				Status: updater.StatusUpdated, UpdateApplied: true,
			}
			return s.deps.RecordUpdate(ctx, activityID, &updater.Result{Updated: 1, Items: []updater.ResourceResult{item}}, nil), nil
		case !unchanged:
			return failed("Interrupted container update effect cannot be confirmed"), nil
		}
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
		return jobcontext.Progress(ctx, scheduler.TargetOutcome{ResourceType: "container", ID: input.ContainerID, Status: scheduler.Running, RecoveryData: data, ActivityID: activityID})
	}
	if recordErr := record(); recordErr != nil {
		return nil, recordErr
	}
	ctx = s.deps.WithFrozenTarget(ctx, &target, record)
	result, runErr := func() (out *updater.Result, err error) {
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
