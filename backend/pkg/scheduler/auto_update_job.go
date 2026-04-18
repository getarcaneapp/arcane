package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
)

type AutoUpdateJob struct {
	updaterService  *services.UpdaterService
	settingsService *services.SettingsService
}

func NewAutoUpdateJob(updaterService *services.UpdaterService, settingsService *services.SettingsService) *AutoUpdateJob {
	return &AutoUpdateJob{
		updaterService:  updaterService,
		settingsService: settingsService,
	}
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
	if j.settingsService.GetBoolSetting(ctx, "autoUpdateWindowEnabled", false) {
		return j.settingsService.GetStringSetting(ctx, "autoUpdateWindowInterval", "*/5 * * * * *")
	}

	s := j.settingsService.GetStringSetting(ctx, "autoUpdateInterval", "0 0 0 * * *")
	if s == "" {
		return "0 0 0 * * *"
	}

	// Handle legacy straight int if it somehow didn't get migrated
	if i, err := strconv.Atoi(s); err == nil {
		if i <= 0 {
			i = 1440
		}
		if i%1440 == 0 {
			return fmt.Sprintf("0 0 0 */%d * *", i/1440)
		}
		if i%60 == 0 {
			return fmt.Sprintf("0 0 */%d * * *", i/60)
		}
		return fmt.Sprintf("0 */%d * * * *", i)
	}

	return s
}

func (j *AutoUpdateJob) Run(ctx context.Context) {
	j.runAt(ctx, time.Now())
}

func (j *AutoUpdateJob) runAt(ctx context.Context, now time.Time) {
	enabled := j.settingsService.GetBoolSetting(ctx, "autoUpdate", false)
	pollingEnabled := j.settingsService.GetBoolSetting(ctx, "pollingEnabled", true)
	if !enabled || !pollingEnabled {
		slog.DebugContext(ctx, "auto-update disabled or polling disabled; skipping run",
			"autoUpdate", enabled, "pollingEnabled", pollingEnabled)
		return
	}

	if j.settingsService.GetBoolSetting(ctx, "autoUpdateWindowEnabled", false) {
		if !j.isWithinWindow(ctx, now) {
			slog.DebugContext(ctx, "auto-update skipped: outside configured time window")
			return
		}
	}

	slog.InfoContext(ctx, "auto-update run started")

	result, err := j.updaterService.ApplyPending(ctx, false)
	if err != nil {
		slog.ErrorContext(ctx, "auto-update run failed", "err", err)
		return
	}

	slog.InfoContext(ctx, "auto-update run completed",
		"checked", result.Checked,
		"updated", result.Updated,
		"skipped", result.Skipped,
		"failed", result.Failed,
	)
}

func (j *AutoUpdateJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling auto-update job in new scheduler; currently requires restart")
	return nil
}

// isWithinWindow reports whether now falls within the configured update window.
// Reads autoUpdateWindowStart (HH:MM), autoUpdateWindowEnd (HH:MM), and
// autoUpdateWindowDays (CSV of 0=Sun…6=Sat). Overnight ranges (start > end)
// wrap midnight correctly.
func (j *AutoUpdateJob) isWithinWindow(ctx context.Context, now time.Time) bool {
	startStr := j.settingsService.GetStringSetting(ctx, "autoUpdateWindowStart", "02:00")
	endStr := j.settingsService.GetStringSetting(ctx, "autoUpdateWindowEnd", "04:00")
	daysStr := j.settingsService.GetStringSetting(ctx, "autoUpdateWindowDays", "0,1,2,3,4,5,6")

	parseHHMM := func(s string) (h, m int, ok bool) {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) != 2 {
			return 0, 0, false
		}
		var err error
		h, err = strconv.Atoi(parts[0])
		if err != nil || h < 0 || h > 23 {
			return 0, 0, false
		}
		m, err = strconv.Atoi(parts[1])
		if err != nil || m < 0 || m > 59 {
			return 0, 0, false
		}
		return h, m, true
	}

	startH, startM, ok1 := parseHHMM(startStr)
	endH, endM, ok2 := parseHHMM(endStr)
	if !ok1 || !ok2 {
		slog.WarnContext(ctx, "auto-update window: invalid time format; allowing update",
			"start", startStr, "end", endStr)
		return true
	}

	// Check day-of-week filter
	allowedDays := make(map[time.Weekday]bool)
	for part := range strings.SplitSeq(daysStr, ",") {
		part = strings.TrimSpace(part)
		if d, err := strconv.Atoi(part); err == nil && d >= 0 && d <= 6 {
			allowedDays[time.Weekday(d)] = true
		}
	}
	if len(allowedDays) > 0 && !allowedDays[now.Weekday()] {
		return false
	}

	nowMins := now.Hour()*60 + now.Minute()
	startMins := startH*60 + startM
	endMins := endH*60 + endM

	if startMins < endMins {
		// Normal range: e.g. 02:00–04:00
		return nowMins >= startMins && nowMins < endMins
	}
	// Overnight range: e.g. 23:00–01:00
	return nowMins >= startMins || nowMins < endMins
}
