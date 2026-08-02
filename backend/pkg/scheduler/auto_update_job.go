package scheduler

import (
	"context"
	"log/slog"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/types/v2/updater"
)

const autoUpdateAdmissionScopeInternal = "auto-update"

// pendingUpdateApplierInternal is the slice of UpdaterService the job needs,
// kept as an interface so the overlap guard is testable with a fake.
type pendingUpdateApplierInternal interface {
	ApplyPending(ctx context.Context, options updater.Options) (*updater.Result, error)
}

type AutoUpdateJob struct {
	updaterService  pendingUpdateApplierInternal
	settingsService *services.SettingsService
	admissionGate   *actors.Gate[actors.AdmissionKey]
}

func NewAutoUpdateJob(updaterService *services.UpdaterService, settingsService *services.SettingsService, admissionGate *actors.Gate[actors.AdmissionKey]) (*AutoUpdateJob, error) {
	if admissionGate == nil {
		return nil, errors.New("auto-update admission gate unavailable")
	}
	return &AutoUpdateJob{
		updaterService:  updaterService,
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

func (j *AutoUpdateJob) Run(ctx context.Context) {
	enabled := j.settingsService.GetBoolSetting(ctx, "autoUpdate", false)
	pollingEnabled := j.settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
	if !enabled || !pollingEnabled {
		slog.DebugContext(ctx, "auto-update disabled or polling disabled; skipping run",
			"autoUpdate", enabled, "pollingEnabled", pollingEnabled)
		return
	}

	lease, admitted, err := j.admissionGate.TryAcquire(ctx, actors.AdmissionKey{Scope: autoUpdateAdmissionScopeInternal})
	if err != nil {
		slog.ErrorContext(ctx, "auto-update admission failed", "error", err)
		return
	}
	if !admitted {
		slog.WarnContext(ctx, "auto-update run still in progress; skipping overlapping run")
		return
	}
	defer lease.Release()

	slog.InfoContext(ctx, "auto-update run started")

	result, err := j.updaterService.ApplyPending(ctx, updater.Options{})
	if err != nil {
		slog.ErrorContext(ctx, "auto-update run failed", "err", err)
		return
	}

	slog.InfoContext(ctx, "auto-update run completed",
		"checked", result.Checked,
		"updated", result.Updated,
		"restarted", result.Restarted,
		"skipped", result.Skipped,
		"failed", result.Failed,
	)
}

func (j *AutoUpdateJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling auto-update job in new scheduler; currently requires restart")
	return nil
}
