package transfer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	transferlib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/transfer"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	transfertypes "github.com/getarcaneapp/arcane/types/v2/transfer"
	"gorm.io/gorm"
)

const (
	jobPrefixInternal      = "resource-transfer:"
	admissionScopeInternal = "resource-transfer"
	runTriggerInternal     = "transfer"
)

// Service coordinates transfers on the manager and serves the per-node
// operations on every environment.
type Service struct {
	db           *database.DB
	cfg          *config.Config
	docker       *docker.DockerClientService
	volumes      *volume.VolumeService
	projects     *project.ProjectService
	uploads      *upload.UploadService
	environments *environment.EnvironmentService
	activity     *activity.ActivityService
	roles        *role.RoleService
	settings     *settings.SettingsService
	holds        *transferlib.Holds
	spool        *transferlib.Spool
	jobs         *entityjobs.Registry
}

// NewService wires the service over its collaborators.
func NewService(deps Dependencies) *Service {
	return &Service{
		db:           deps.DB,
		cfg:          deps.Config,
		docker:       deps.Docker,
		volumes:      deps.Volume,
		projects:     deps.Project,
		uploads:      deps.Upload,
		environments: deps.Environment,
		activity:     deps.Activity,
		roles:        deps.Role,
		settings:     deps.Settings,
		holds:        transferlib.NewHolds(deps.KV),
		spool:        transferlib.NewSpool(filepath.Join(os.TempDir(), "arcane-transfers"), transfertypes.ExportIdleTimeout),
		jobs:         entityjobs.New(jobPrefixInternal, admissionScopeInternal),
	}
}

// Start begins staged-export housekeeping for the process lifetime.
func (s *Service) Start(ctx context.Context) { s.spool.Start(ctx) }

// SetScheduler injects the durable scheduler and re-registers every
// unfinished transfer so interrupted runs reconcile before new work starts.
func (s *Service) SetScheduler(ctx context.Context, scheduler schedulertypes.DynamicScheduler, admission *actors.Gate[actors.AdmissionKey]) error {
	if err := s.jobs.SetScheduler(ctx, scheduler, admission); err != nil {
		return err
	}
	var pending []ResourceTransfer
	if err := s.db.WithContext(ctx).Where("status IN ?", []transfertypes.Status{transfertypes.StatusQueued, transfertypes.StatusRunning}).Find(&pending).Error; err != nil {
		return fmt.Errorf("load unfinished transfers: %w", err)
	}
	for i := range pending {
		s.registerJobInternal(ctx, pending[i].ID)
		if pending[i].Status == transfertypes.StatusRunning {
			if err := s.markInterruptedInternal(ctx, &pending[i]); err != nil {
				slog.WarnContext(ctx, "transfer: failed to mark interrupted transfer", "transfer", pending[i].ID, "error", err)
			}
		}
	}
	return nil
}

func (s *Service) registerJobInternal(ctx context.Context, transferID string) {
	s.jobs.Register(ctx, transferID,
		func(context.Context) string { return "" },
		func(runCtx context.Context) (schedulertypes.Outcome, error) { return s.runInternal(runCtx, transferID) },
		func(runCtx context.Context, previous schedulertypes.Run) (schedulertypes.Outcome, error) {
			return s.reconcileInternal(runCtx, transferID, previous)
		},
	)
}

func (s *Service) submitRunInternal(ctx context.Context, transferID string) error {
	s.registerJobInternal(ctx, transferID)
	if s.jobs.Scheduler() == nil {
		return errors.New("transfer scheduler is unavailable")
	}
	_, err := s.jobs.Scheduler().Submit(ctx, schedulertypes.Request{JobID: s.jobs.JobName(transferID), EnvironmentID: "0", Trigger: runTriggerInternal})
	return err
}

func (s *Service) loadInternal(ctx context.Context, transferID string) (*ResourceTransfer, error) {
	var record ResourceTransfer
	if err := s.db.WithContext(ctx).First(&record, "id = ?", transferID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.ErrTransferNotFound
		}
		return nil, err
	}
	return &record, nil
}

// updateInternal applies change under a fresh read so concurrent updates
// (progress, cancel requests) never overwrite each other.
func (s *Service) updateInternal(ctx context.Context, transferID string, change func(*ResourceTransfer)) (*ResourceTransfer, error) {
	var updated *ResourceTransfer
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record ResourceTransfer
		if err := tx.First(&record, "id = ?", transferID).Error; err != nil {
			return err
		}
		change(&record)
		now := time.Now().UTC()
		record.UpdatedAt = &now
		if err := tx.Save(&record).Error; err != nil {
			return err
		}
		updated = &record
		return nil
	})
	return updated, err
}

// Get returns one transfer scoped to its source environment and kind.
func (s *Service) Get(ctx context.Context, sourceEnvID string, kind transfertypes.Kind, transferID string) (*ResourceTransfer, error) {
	record, err := s.loadInternal(ctx, transferID)
	if err != nil {
		return nil, err
	}
	if record.SourceEnvironmentID != sourceEnvID || record.Kind != kind {
		return nil, common.ErrTransferNotFound
	}
	return record, nil
}

// List returns the transfers of one kind whose source is the environment, newest first.
func (s *Service) List(ctx context.Context, sourceEnvID string, kind transfertypes.Kind) ([]ResourceTransfer, error) {
	var records []ResourceTransfer
	err := s.db.WithContext(ctx).Where("source_environment_id = ? AND kind = ?", sourceEnvID, kind).Order("created_at DESC").Limit(200).Find(&records).Error
	return records, err
}

// Preflight builds the plan the user reviews. It is stateless.
func (s *Service) Preflight(ctx context.Context, sourceEnvID string, request transfertypes.Request, permissions *authz.PermissionSet) (transfertypes.Plan, error) {
	if !s.settings.GetBoolSetting(ctx, "experimentalFeaturesEnabled", false) {
		return transfertypes.Plan{}, common.Classify(common.ErrFeatureDisabled, errors.New("transfers are experimental; enable experimental features first"))
	}
	return s.buildPlanInternal(ctx, sourceEnvID, request, permissions)
}

// Create persists an approved plan and queues its run. The second result is
// false when the idempotency key matched an existing transfer.
func (s *Service) Create(ctx context.Context, sourceEnvID string, request transfertypes.CreateRequest, user *common.User, permissions *authz.PermissionSet) (*ResourceTransfer, bool, error) {
	if !s.settings.GetBoolSetting(ctx, "experimentalFeaturesEnabled", false) {
		return nil, false, common.Classify(common.ErrFeatureDisabled, errors.New("transfers are experimental; enable experimental features first"))
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	var existing []ResourceTransfer
	if err := s.db.WithContext(ctx).Where("idempotency_key = ?", key).Limit(1).Find(&existing).Error; err != nil {
		return nil, false, err
	}
	if len(existing) == 1 {
		return &existing[0], false, nil
	}
	plan, err := s.buildPlanInternal(ctx, sourceEnvID, request.Request, permissions)
	if err != nil {
		return nil, false, err
	}
	if plan.PlanHash != strings.TrimSpace(request.PlanHash) {
		return nil, false, common.ErrTransferPlanChanged
	}
	if plan.Blocked() {
		return nil, false, common.Classify(common.ErrTransferBlocked, fmt.Errorf("%w: %s", common.ErrTransferBlocked, plan.Blockers[0].Message))
	}
	if missing := missingAcknowledgementsInternal(plan, request.Acknowledgements); len(missing) > 0 {
		return nil, false, common.Classify(common.ErrValidation, fmt.Errorf("acknowledge %s before starting the transfer", strings.Join(missing, ", ")))
	}
	record := &ResourceTransfer{
		IdempotencyKey:           key,
		Kind:                     plan.Request.Kind,
		Mode:                     plan.Request.Mode,
		SourceEnvironmentID:      sourceEnvID,
		DestinationEnvironmentID: plan.Request.DestinationEnvironmentID,
		Status:                   transfertypes.StatusQueued,
		Phase:                    transfertypes.PhasePending,
		Plan:                     plan,
		Resources:                initialResourcesInternal(plan),
		RecordedConsumers:        []transfertypes.Consumer{},
		RequestedBy:              user.ID,
	}
	if err := s.db.WithContext(ctx).Create(record).Error; err != nil {
		return nil, false, fmt.Errorf("persist transfer: %w", err)
	}
	if err := s.submitRunInternal(ctx, record.ID); err != nil {
		_, _ = s.updateInternal(ctx, record.ID, func(t *ResourceTransfer) {
			t.Status = transfertypes.StatusFailed
			t.Error = "failed to queue transfer: " + err.Error()
		})
		return nil, false, err
	}
	return record, true, nil
}

func missingAcknowledgementsInternal(plan transfertypes.Plan, given []string) []string {
	have := map[string]struct{}{}
	for _, code := range given {
		have[strings.TrimSpace(code)] = struct{}{}
	}
	var missing []string
	for _, code := range plan.RequiredAcknowledgements {
		if _, ok := have[code]; !ok {
			missing = append(missing, code)
		}
	}
	return missing
}

func initialResourcesInternal(plan transfertypes.Plan) []transfertypes.ResourceProgress {
	resources := make([]transfertypes.ResourceProgress, 0, len(plan.Resources))
	for _, planned := range plan.Resources {
		resources = append(resources, transfertypes.ResourceProgress{
			Key:             planned.Key,
			Kind:            planned.Kind,
			SourceName:      planned.SourceName,
			DestinationName: planned.DestinationName,
			Status:          transfertypes.ResourcePending,
			BytesTotal:      planned.EstimatedBytes,
		})
	}
	return resources
}

// requesterPermissionsInternal re-resolves the requesting user's permissions
// so queued and later actions never rely on the permissions at creation.
func (s *Service) requesterPermissionsInternal(ctx context.Context, userID string) (*authz.PermissionSet, error) {
	var user common.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return nil, fmt.Errorf("requesting user is unavailable: %w", err)
	}
	return s.roles.ResolveUserPermissionsInDB(ctx, s.db.WithContext(ctx), user.ID)
}

// Cancel stops a queued or running transfer. Running transfers finish their
// current step and then recover.
func (s *Service) Cancel(ctx context.Context, sourceEnvID string, kind transfertypes.Kind, transferID string, permissions *authz.PermissionSet) (*ResourceTransfer, error) {
	record, err := s.Get(ctx, sourceEnvID, kind, transferID)
	if err != nil {
		return nil, err
	}
	if err := authorizeActionInternal(permissions, record); err != nil {
		return nil, err
	}
	switch record.Status { //nolint:exhaustive // finished and attention states cannot be canceled; the default explains the alternatives
	case transfertypes.StatusQueued:
		updated, err := s.updateInternal(ctx, transferID, func(t *ResourceTransfer) {
			t.Status = transfertypes.StatusCanceled
			t.CancelRequested = true
			now := time.Now().UTC()
			t.FinishedAt = &now
			t.Phase = transfertypes.PhaseFinished
		})
		if err != nil {
			return nil, err
		}
		s.jobs.Unregister(ctx, transferID)
		return updated, nil
	case transfertypes.StatusRunning:
		updated, err := s.updateInternal(ctx, transferID, func(t *ResourceTransfer) { t.CancelRequested = true })
		if err != nil {
			return nil, err
		}
		if updated.ActivityID != "" {
			s.activity.RequestCancel(updated.ActivityID)
		}
		return updated, nil
	default:
		return nil, common.Classify(common.ErrTransferStateInvalid, fmt.Errorf("transfer is %s; use retry or rollback instead", record.Status))
	}
}

// Retry requeues a failed transfer or one that needs attention.
func (s *Service) Retry(ctx context.Context, sourceEnvID string, kind transfertypes.Kind, transferID string, permissions *authz.PermissionSet) (*ResourceTransfer, error) {
	record, err := s.Get(ctx, sourceEnvID, kind, transferID)
	if err != nil {
		return nil, err
	}
	if err := authorizeActionInternal(permissions, record); err != nil {
		return nil, err
	}
	if record.Status != transfertypes.StatusFailed && record.Status != transfertypes.StatusNeedsAttention {
		return nil, common.Classify(common.ErrTransferStateInvalid, fmt.Errorf("transfer is %s and cannot be retried", record.Status))
	}
	if record.DestinationStartupAttempted {
		return nil, common.Classify(common.ErrTransferStateInvalid, errors.New("the destination was started; review it and roll back or release the source hold instead of retrying"))
	}
	updated, err := s.updateInternal(ctx, transferID, func(t *ResourceTransfer) {
		t.Status = transfertypes.StatusQueued
		t.CancelRequested = false
		t.Error = ""
		t.FinishedAt = nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.submitRunInternal(ctx, transferID); err != nil {
		return nil, err
	}
	return updated, nil
}

// ReleaseHold records an explicit decision to resume the source after a Move.
func (s *Service) ReleaseHold(ctx context.Context, sourceEnvID string, kind transfertypes.Kind, transferID string, permissions *authz.PermissionSet) (*ResourceTransfer, error) {
	record, err := s.Get(ctx, sourceEnvID, kind, transferID)
	if err != nil {
		return nil, err
	}
	if err := authorizeActionInternal(permissions, record); err != nil {
		return nil, err
	}
	if !record.Status.Terminal() && record.Status != transfertypes.StatusNeedsAttention {
		return nil, common.Classify(common.ErrTransferStateInvalid, errors.New("the transfer is still active"))
	}
	if err := s.releaseHoldsInternal(ctx, record, true, true); err != nil {
		return nil, err
	}
	return s.updateInternal(ctx, transferID, func(t *ResourceTransfer) { t.SourceHeld = false })
}

// authorizeActionInternal gates later actions on the transfer permission for
// both environments, re-evaluated with the caller's current permissions.
func authorizeActionInternal(permissions *authz.PermissionSet, record *ResourceTransfer) error {
	perm := transferPermissionInternal(record.Kind)
	if !permissions.Allows(perm, record.SourceEnvironmentID) || !permissions.Allows(perm, record.DestinationEnvironmentID) {
		return common.Classify(common.ErrForbidden, fmt.Errorf("permission denied: %s on both environments", perm))
	}
	return nil
}

func transferPermissionInternal(kind transfertypes.Kind) string {
	if kind == transfertypes.KindProject {
		return authz.PermProjectsTransfer
	}
	return authz.PermVolumesTransfer
}

// authorizePlanInternal checks every permission the plan needs on both
// environments; it returns blockers rather than errors so the review shows them.
func authorizePlanInternal(permissions *authz.PermissionSet, plan *transfertypes.Plan) {
	source := plan.SourceEnvironmentID
	destination := plan.Request.DestinationEnvironmentID
	require := func(perm, envID, what string) {
		if !permissions.Allows(perm, envID) {
			plan.Blockers = append(plan.Blockers, transfertypes.Blocker{Code: "permission", Message: fmt.Sprintf("missing %s on the %s environment", perm, what)})
		}
	}
	transferPerm := transferPermissionInternal(plan.Request.Kind)
	require(transferPerm, source, "source")
	require(transferPerm, destination, "destination")
	if plan.Request.Kind == transfertypes.KindProject {
		require(authz.PermProjectsCreate, destination, "destination")
		require(authz.PermVolumesCreate, destination, "destination")
		if plan.RequiresDowntime {
			require(authz.PermProjectsDown, source, "source")
		}
		if plan.Request.Mode == transfertypes.ModeMove {
			require(authz.PermProjectsDeploy, destination, "destination")
		}
	} else {
		require(authz.PermVolumesCreate, destination, "destination")
		if plan.RequiresDowntime {
			require(authz.PermContainersStop, source, "source")
		}
	}
}
