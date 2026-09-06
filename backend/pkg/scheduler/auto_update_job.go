package scheduler

import (
	"context"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	updatertypes "github.com/getarcaneapp/arcane/types/v2/updater"
)

const autoUpdateAdmissionScopeInternal = "auto-update"

// pendingUpdateApplierInternal is the slice of UpdaterService the job needs,
// kept as an interface so the overlap guard is testable with a fake.
type pendingUpdateApplierInternal interface {
	ApplyPending(ctx context.Context, options updatertypes.Options) (*updatertypes.Result, error)
}

type AutoUpdateJob struct {
	updaterService  pendingUpdateApplierInternal
	settingsService *settings.SettingsService
	admissionGate   *actors.Gate[actors.AdmissionKey]
}

func NewAutoUpdateJob(updaterModule *updater.Module, settingsService *settings.SettingsService, admissionGate *actors.Gate[actors.AdmissionKey]) (*AutoUpdateJob, error) {
	if admissionGate == nil {
		return nil, errors.New("auto-update admission gate unavailable")
	}
	return &AutoUpdateJob{
		updaterService:  updaterModule.Service(),
		settingsService: settingsService,
		admissionGate:   admissionGate,
	}, nil
}

func (j *AutoUpdateJob) Name() string {
	return "auto-update"
}

func (j *AutoUpdateJob) ShouldSchedule(ctx context.Context) bool {
	enabled := j.settingsService.GetBoolSetting(ctx, "autoUpdate", false)
	pollingEnabled := j.settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
	return enabled && pollingEnabled
}

func (j *AutoUpdateJob) Schedule(ctx context.Context) string {
	s := j.settingsService.GetStringSetting(ctx, "autoUpdateInterval", "0 0 0 * * *")
	if s == "" {
		return "0 0 0 * * *"
	}
	return s
}

func (j *AutoUpdateJob) Run(ctx context.Context) (schedulertypes.Outcome, error) {
	options := updatertypes.Options{}
	unresolvedTargets := false
	if previous, ok := jobcontext.Run(ctx); ok {
		var priorOutcome schedulertypes.Outcome
		options, priorOutcome, unresolvedTargets = autoUpdateRetryOptionsInternal(previous)
		if priorOutcome.Status != "" {
			return priorOutcome, nil
		}
	}

	enabled := j.settingsService.GetBoolSetting(ctx, "autoUpdate", false)
	pollingEnabled := j.settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
	if !enabled || !pollingEnabled {
		slog.DebugContext(ctx, "auto-update disabled or polling disabled; skipping run",
			"autoUpdate", enabled, "pollingEnabled", pollingEnabled)
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
	}

	lease, admitted, err := j.admissionGate.TryAcquire(ctx, actors.AdmissionKey{Scope: autoUpdateAdmissionScopeInternal})
	if err != nil {
		slog.ErrorContext(ctx, "auto-update admission failed", "error", err)
		return schedulertypes.Outcome{}, err
	}
	if !admitted {
		slog.WarnContext(ctx, "auto-update run still in progress; skipping overlapping run")
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
	}
	defer lease.Release()

	slog.InfoContext(ctx, "auto-update run started")

	result, err := j.updaterService.ApplyPending(ctx, options)
	if err != nil {
		slog.ErrorContext(ctx, "auto-update run failed", "err", err)
		if result == nil {
			return schedulertypes.Outcome{}, err
		}
	}
	if result == nil {
		return schedulertypes.Outcome{Status: schedulertypes.NeedsAttention, Message: "Updater returned no operation result"}, nil
	}

	slog.InfoContext(ctx, "auto-update run completed",
		"checked", result.Checked,
		"updated", result.Updated,
		"restarted", result.Restarted,
		"skipped", result.Skipped,
		"failed", result.Failed,
	)
	outcome := schedulertypes.Outcome{Status: schedulertypes.Succeeded}
	if result.ActivityID != nil {
		outcome.ActivityID = *result.ActivityID
	}
	for _, item := range result.Items {
		status := schedulertypes.Succeeded
		if item.Status == "failed" {
			status = schedulertypes.Failed
		}
		if item.Status == "skipped" {
			status = schedulertypes.Skipped
		}
		outcome.Targets = append(outcome.Targets, schedulertypes.TargetOutcome{ID: item.ResourceID, ResourceType: item.ResourceType, Status: status, Message: item.Error})
	}
	if result.Failed > 0 || err != nil || unresolvedTargets {
		outcome.Status = schedulertypes.Partial
		outcome.Message = "Some updates failed; review target outcomes before retrying"
	}
	return outcome, err
}

func (j *AutoUpdateJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling auto-update job in new scheduler; currently requires restart")
	return nil
}

func (j *AutoUpdateJob) Reconcile(_ context.Context, previous schedulertypes.Run) (schedulertypes.Outcome, error) {
	return jobcontext.ConfirmedTarget(previous, "auto-update"), nil
}

func autoUpdateRetryOptionsInternal(previous schedulertypes.Run) (updatertypes.Options, schedulertypes.Outcome, bool) {
	options := updatertypes.Options{}
	unresolvedTargets := false
	confirmed := jobcontext.ConfirmedTarget(previous, "auto-update")
	if confirmed.Status == schedulertypes.Succeeded {
		return options, confirmed, false
	}
	hasResourceTargets := false
	for _, target := range previous.Outcome.Targets {
		if target.ID == "auto-update" {
			continue
		}
		hasResourceTargets = true
		if target.Status == schedulertypes.Succeeded || target.Status == schedulertypes.Skipped {
			continue
		}
		if target.ResourceType != "container" || target.Status != schedulertypes.Failed {
			unresolvedTargets = true
			continue
		}
		options.ResourceIds = append(options.ResourceIds, target.ID)
	}
	if hasResourceTargets && len(options.ResourceIds) == 0 {
		if unresolvedTargets {
			return options, schedulertypes.Outcome{Status: schedulertypes.NeedsAttention, Message: "Remaining image or unconfirmed targets need review before retrying", Targets: previous.Outcome.Targets}, true
		}
		return options, schedulertypes.Outcome{Status: schedulertypes.Succeeded, Targets: previous.Outcome.Targets}, false
	}
	if len(options.ResourceIds) > 0 {
		options.Type = "container"
	}
	return options, schedulertypes.Outcome{}, unresolvedTargets
}
