package scheduler

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	scheduleutil "github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/schedule"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker/compat"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"golang.org/x/sync/errgroup"
)

const (
	AutoHealJobName                = "auto-heal"
	autoHealDefaultSchedule        = "0 */5 * * * *"
	autoHealInspectConcurrency     = 4
	autoHealAdmissionScopeInternal = "auto-heal"
)

// restartRecord tracks restart timestamps for a single container.
type autoHealPlanInternal struct {
	Containers    []container.Summary `json:"containers"`
	MaxRestarts   int                 `json:"maxRestarts"`
	WindowMinutes int                 `json:"windowMinutes"`
}

type autoHealBaselineInternal struct {
	StartedAt    string `json:"startedAt"`
	RestartCount int    `json:"restartCount"`
}

type restartRecord struct {
	timestamps []time.Time
}

type AutoHealJob struct {
	dockerClientService *docker.DockerClientService
	settingsService     *settings.SettingsService
	eventService        *event.EventService
	notificationService *notification.NotificationService
	admissionGate       *runs.Admission

	mu       sync.Mutex
	restarts map[string]*restartRecord

	selfIDOnce sync.Once
	selfID     string

	getDockerClient    func() (*client.Client, error)
	listContainers     func(ctx context.Context, dockerClient *client.Client) ([]container.Summary, error)
	inspectContainer   func(ctx context.Context, dockerClient *client.Client, containerID string) (container.InspectResponse, error)
	restartContainer   func(ctx context.Context, dockerClient *client.Client, containerID string) error
	getSelfContainerID func() (string, error)
}

func NewAutoHealJob(
	dockerClientService *docker.DockerClientService,
	settingsService *settings.SettingsService,
	eventService *event.EventService,
	notificationService *notification.NotificationService,
	admissionGate *runs.Admission,
) (*AutoHealJob, error) {
	if admissionGate == nil {
		return nil, errors.New("auto-heal admission gate unavailable")
	}
	return &AutoHealJob{
		dockerClientService: dockerClientService,
		settingsService:     settingsService,
		eventService:        eventService,
		notificationService: notificationService,
		admissionGate:       admissionGate,
		restarts:            make(map[string]*restartRecord),
	}, nil
}

func (j *AutoHealJob) Name() string {
	return AutoHealJobName
}

func (j *AutoHealJob) ShouldSchedule(ctx context.Context) bool {
	return j.settingsService.GetBoolSetting(ctx, "autoHealEnabled", false)
}

func (j *AutoHealJob) Schedule(ctx context.Context) string {
	schedule := cmp.Or(j.settingsService.GetStringSetting(ctx, "autoHealInterval", autoHealDefaultSchedule), autoHealDefaultSchedule)

	parser := scheduleutil.Parser()
	if _, err := parser.Parse(schedule); err != nil {
		slog.WarnContext(ctx, "Invalid cron expression for auto-heal, using default", "invalid_schedule", schedule, "error", err)
		return autoHealDefaultSchedule
	}

	return schedule
}

func (j *AutoHealJob) Run(ctx context.Context) (schedulertypes.Outcome, error) {
	enabled := j.settingsService.GetBoolSetting(ctx, "autoHealEnabled", false)
	if !enabled {
		slog.DebugContext(ctx, "auto-heal disabled; skipping run")
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
	}

	lease, admitted, err := j.admissionGate.TryAcquire(ctx, schedulertypes.AdmissionKey{Scope: autoHealAdmissionScopeInternal})
	if err != nil {
		slog.ErrorContext(ctx, "auto-heal admission failed", "error", err)
		return schedulertypes.Outcome{}, err
	}
	if !admitted {
		slog.WarnContext(ctx, "auto-heal run still in progress; skipping overlapping run")
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
	}
	defer lease.Release(ctx)

	dockerClient, err := j.getDockerClientInternal(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "auto-heal failed to get Docker client", "error", err)
		return schedulertypes.Outcome{}, err
	}

	containerList, err := j.ListContainers(ctx, dockerClient)
	if err != nil {
		slog.ErrorContext(ctx, "auto-heal failed to list containers", "error", err)
		return schedulertypes.Outcome{}, err
	}
	containers := containerList

	excludedContainers := j.parseExcludedContainers(ctx)
	maxRestarts := j.settingsService.GetIntSetting(ctx, "autoHealMaxRestarts", 5)
	restartWindowMinutes := j.settingsService.GetIntSetting(ctx, "autoHealRestartWindow", 30)
	restartWindow := time.Duration(restartWindowMinutes) * time.Minute

	selfID := j.selfContainerIDInternal(ctx)
	candidates := j.filterCandidatesInternal(containers, excludedContainers, selfID)
	frozen, err := json.Marshal(autoHealPlanInternal{Containers: candidates, MaxRestarts: maxRestarts, WindowMinutes: restartWindowMinutes})
	if err != nil {
		return schedulertypes.Outcome{}, err
	}
	if err := jobcontext.Progress(ctx, schedulertypes.TargetOutcome{ResourceType: "heal_plan", ID: "auto-heal-plan", Status: schedulertypes.Succeeded, RecoveryData: frozen}); err != nil {
		return schedulertypes.Outcome{}, err
	}

	g, groupCtx := errgroup.WithContext(ctx)
	var resultMu sync.Mutex
	outcome := schedulertypes.Outcome{Status: schedulertypes.Succeeded}
	g.SetLimit(autoHealInspectConcurrency)

	for _, candidate := range candidates {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "auto-heal worker")

			target, err := j.processCandidateInternal(groupCtx, dockerClient, candidate, maxRestarts, restartWindow, restartWindowMinutes)
			resultMu.Lock()
			outcome.Targets = append(outcome.Targets, target)
			if err != nil {
				outcome.Status = schedulertypes.Partial
			}
			resultMu.Unlock()
			if progressErr := jobcontext.Progress(groupCtx, target); progressErr != nil {
				return progressErr
			}
			return nil
		})
	}

	err = g.Wait()
	return outcome, err
}

// selfContainerIDInternal resolves and caches the ID (full 64-char or short
// prefix) of the container Arcane itself runs in. Returns "" when detection
// fails (e.g. binary running directly on the host), disabling the guard.
func (j *AutoHealJob) selfContainerIDInternal(ctx context.Context) string {
	j.selfIDOnce.Do(func() {
		detect := j.getSelfContainerID
		if detect == nil {
			detect = cgroup.CurrentContainerID
		}
		id, err := detect()
		if err != nil {
			slog.DebugContext(ctx, "auto-heal: could not determine own container ID; self-protection disabled", "error", err)
			return
		}
		j.selfID = strings.ToLower(strings.TrimSpace(id))
		slog.InfoContext(ctx, "auto-heal: detected own container; it will never be auto-restarted", "container_id", j.selfID)
	})
	return j.selfID
}

func (j *AutoHealJob) filterCandidatesInternal(containers []container.Summary, excludedContainers map[string]struct{}, selfID string) []container.Summary {
	candidates := make([]container.Summary, 0, len(containers))
	for _, c := range containers {
		// Never restart the container Arcane itself runs in: a slow or
		// mid-startup manager that trips its own healthcheck would otherwise
		// be restarted by its own auto-heal, in a loop. Prefix match because
		// hostname-based detection yields the short 12-char ID.
		if selfID != "" && strings.HasPrefix(strings.ToLower(c.ID), selfID) {
			continue
		}

		if internal, _ := kit.ParseBool(c.Labels[libarcane.InternalResourceLabel]); internal {
			continue
		}

		containerName := dockerutil.ContainerNameFromNames(c.Names)
		if j.isExcluded(containerName, excludedContainers) {
			continue
		}

		candidates = append(candidates, c)
	}

	return candidates
}

func (j *AutoHealJob) processCandidateInternal(
	ctx context.Context,
	dockerClient *client.Client,
	candidate container.Summary,
	maxRestarts int,
	restartWindow time.Duration,
	restartWindowMinutes int,
) (schedulertypes.TargetOutcome, error) {
	containerID := candidate.ID
	target := schedulertypes.TargetOutcome{ResourceType: "container", ID: containerID, Status: schedulertypes.Skipped}
	if previous, ok := jobcontext.Run(ctx); ok {
		for _, completed := range previous.Outcome.Targets {
			if completed.ID == containerID && completed.Status == schedulertypes.Succeeded {
				return completed, nil
			}
		}
	}
	containerName := dockerutil.ContainerNameFromNames(candidate.Names)

	// The daemon-side health filter already selected unhealthy containers; the
	// inspect re-confirms right before restarting so a container that recovered
	// (or was restarted by someone else) since the list isn't bounced again.
	inspect, err := j.inspectContainerInternal(ctx, dockerClient, containerID)
	if err != nil {
		slog.WarnContext(ctx, "auto-heal failed to inspect container", "container", containerName, "error", err)
		target.Status = schedulertypes.Failed
		target.Message = err.Error()
		return target, err
	}

	if inspect.State == nil || inspect.State.Health == nil {
		return target, nil
	}
	if inspect.State.Health.Status != container.Unhealthy {
		return target, nil
	}

	releaseSlot, reserved := j.reserveRestartSlotInternal(containerID, maxRestarts, restartWindow)
	if !reserved {
		slog.WarnContext(
			ctx, "auto-heal restart-loop protection: skipping container",
			"container", containerName,
			"max_restarts", maxRestarts,
			"window_minutes", restartWindowMinutes,
		)
		return target, nil
	}

	baseline, err := json.Marshal(autoHealBaselineInternal{StartedAt: inspect.State.StartedAt, RestartCount: inspect.RestartCount})
	if err != nil {
		releaseSlot()
		return target, err
	}
	target.RecoveryData = baseline
	target.Status = schedulertypes.Running
	if err := jobcontext.Progress(ctx, target); err != nil {
		releaseSlot()
		return target, err
	}
	if err := j.restartContainerInternal(ctx, dockerClient, containerID); err != nil {
		releaseSlot()
		slog.ErrorContext(ctx, "auto-heal failed to restart container", "container", containerName, "error", err)
		target.Status = schedulertypes.Failed
		target.Message = err.Error()
		return target, err
	}

	j.postRestartActionsInternal(ctx, containerID, containerName)

	slog.InfoContext(ctx, "auto-heal restarted unhealthy container", "container", containerName, "container_id", containerID)
	target.Status = schedulertypes.Succeeded
	return target, nil
}

func (j *AutoHealJob) postRestartActionsInternal(ctx context.Context, containerID, containerName string) {
	if j.eventService != nil {
		if err := j.eventService.LogContainerEvent(
			ctx,
			event.EventTypeContainerRestart,
			containerID,
			containerName,
			"", // no user - system action
			"system",
			"",
			database.JSON{"action": "auto-heal", "reason": "unhealthy"},
		); err != nil {
			slog.WarnContext(ctx, "auto-heal failed to log event", "container", containerName, "error", err)
		}
	}

	if j.notificationService != nil {
		if err := j.notificationService.SendAutoHealNotification(ctx, containerName, containerID); err != nil {
			slog.WarnContext(ctx, "auto-heal failed to send notification", "container", containerName, "error", err)
		}
	}
}

func (j *AutoHealJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling auto-heal job in new scheduler; currently requires restart")
	return nil
}

// pruneRecordLockedInternal prunes expired timestamps for containerID and drops
// the entry entirely when nothing recent remains, so restarts does not grow
// without bound as containers come and go. Callers must hold j.mu.
func (j *AutoHealJob) pruneRecordLockedInternal(containerID string, cutoff time.Time) *restartRecord {
	record, exists := j.restarts[containerID]
	if !exists {
		return nil
	}

	record.timestamps = j.pruneTimestamps(record.timestamps, cutoff)
	if len(record.timestamps) == 0 {
		delete(j.restarts, containerID)
		return nil
	}
	return record
}

// reserveRestartSlotInternal atomically checks the rate limit and claims a slot.
// Checking and recording separately let two workers inspecting the same
// container both pass the check and restart it past the configured limit.
//
// The returned release undoes the reservation, so a restart that fails does not
// consume a slot — matching the previous behaviour of only counting restarts
// that actually happened.
func (j *AutoHealJob) reserveRestartSlotInternal(containerID string, maxRestarts int, window time.Duration) (func(), bool) {
	j.mu.Lock()
	defer j.mu.Unlock()

	record := j.pruneRecordLockedInternal(containerID, time.Now().Add(-window))
	if record != nil && len(record.timestamps) >= maxRestarts {
		return nil, false
	}
	if record == nil {
		record = &restartRecord{}
		j.restarts[containerID] = record
	}

	reserved := time.Now()
	record.timestamps = append(record.timestamps, reserved)

	return func() { j.releaseRestartSlotInternal(containerID, reserved) }, true
}

func (j *AutoHealJob) releaseRestartSlotInternal(containerID string, reserved time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()

	record, exists := j.restarts[containerID]
	if !exists {
		return
	}
	for i, ts := range slices.Backward(record.timestamps) {
		if ts.Equal(reserved) {
			record.timestamps = append(record.timestamps[:i], record.timestamps[i+1:]...)
			break
		}
	}
	if len(record.timestamps) == 0 {
		delete(j.restarts, containerID)
	}
}

// canRestart checks if a container can be restarted within the rate limit.
func (j *AutoHealJob) canRestart(containerID string, maxRestarts int, window time.Duration) bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	record := j.pruneRecordLockedInternal(containerID, time.Now().Add(-window))
	if record == nil {
		return true
	}

	return len(record.timestamps) < maxRestarts
}

// recordRestart records a restart timestamp for a container.
func (j *AutoHealJob) recordRestart(containerID string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	record, exists := j.restarts[containerID]
	if !exists {
		record = &restartRecord{}
		j.restarts[containerID] = record
	}

	record.timestamps = append(record.timestamps, time.Now())
}

// pruneTimestamps removes timestamps older than the cutoff.
func (j *AutoHealJob) pruneTimestamps(timestamps []time.Time, cutoff time.Time) []time.Time {
	result := make([]time.Time, 0, len(timestamps))
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			result = append(result, ts)
		}
	}
	return result
}

func (j *AutoHealJob) parseExcludedContainers(ctx context.Context) map[string]struct{} {
	raw := j.settingsService.GetStringSetting(ctx, "autoHealExcludedContainers", "")
	excluded := make(map[string]struct{})
	if raw == "" {
		return excluded
	}
	for name := range strings.SplitSeq(raw, ",") {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			excluded[trimmed] = struct{}{}
		}
	}
	return excluded
}

func (j *AutoHealJob) isExcluded(name string, excluded map[string]struct{}) bool {
	_, ok := excluded[name]
	return ok
}

func (j *AutoHealJob) inspectContainerInternal(ctx context.Context, dockerClient *client.Client, containerID string) (container.InspectResponse, error) {
	if j.inspectContainer != nil {
		return j.inspectContainer(ctx, dockerClient, containerID)
	}

	inspect, err := compat.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return container.InspectResponse{}, err
	}

	return inspect.Container, nil
}

func (j *AutoHealJob) restartContainerInternal(ctx context.Context, dockerClient *client.Client, containerID string) error {
	if j.restartContainer != nil {
		return j.restartContainer(ctx, dockerClient, containerID)
	}

	_, err := dockerClient.ContainerRestart(ctx, containerID, client.ContainerRestartOptions{})
	return err
}

func (j *AutoHealJob) getDockerClientInternal(ctx context.Context) (*client.Client, error) {
	if j.getDockerClient != nil {
		return j.getDockerClient()
	}

	return j.dockerClientService.GetClient(ctx)
}

// autoHealListOptionsInternal filters unhealthy containers at the daemon, so
// the common all-healthy case returns an empty list and nothing is inspected.
func autoHealListOptionsInternal() client.ContainerListOptions {
	return client.ContainerListOptions{
		All:     false,
		Filters: make(client.Filters).Add("health", string(container.Unhealthy)),
	}
}

func (j *AutoHealJob) ListContainers(ctx context.Context, dockerClient *client.Client) ([]container.Summary, error) {
	if j.listContainers != nil {
		return j.listContainers(ctx, dockerClient)
	}

	containerList, err := dockerClient.ContainerList(ctx, autoHealListOptionsInternal())
	if err != nil {
		return nil, err
	}

	return containerList.Items, nil
}

// ResetRestartTracking clears all restart records (exported for testing).
func (j *AutoHealJob) ResetRestartTracking() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.restarts = make(map[string]*restartRecord)
}

// CanRestartExported exposes canRestart for testing.
func (j *AutoHealJob) CanRestartExported(containerID string, maxRestarts int, window time.Duration) bool {
	return j.canRestart(containerID, maxRestarts, window)
}

// RecordRestartExported exposes recordRestart for testing.
func (j *AutoHealJob) RecordRestartExported(containerID string) {
	j.recordRestart(containerID)
}

// RecordRestartAtExported records a restart at a specific time for testing.
func (j *AutoHealJob) RecordRestartAtExported(containerID string, t time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()

	record, exists := j.restarts[containerID]
	if !exists {
		record = &restartRecord{}
		j.restarts[containerID] = record
	}

	record.timestamps = append(record.timestamps, t)
}

func (j *AutoHealJob) Reconcile(ctx context.Context, previous schedulertypes.Run) (schedulertypes.Outcome, error) {
	lease, admitted, err := j.admissionGate.TryAcquire(ctx, schedulertypes.AdmissionKey{Scope: autoHealAdmissionScopeInternal})
	if err != nil {
		return schedulertypes.Outcome{}, err
	}
	if !admitted {
		return schedulertypes.Outcome{Status: schedulertypes.NeedsAttention, Message: "A container healing run already owns admission"}, nil
	}
	defer lease.Release(ctx)
	var plan autoHealPlanInternal
	planned := false
	for _, target := range previous.Outcome.Targets {
		if target.ID == "auto-heal-plan" && len(target.RecoveryData) > 0 {
			if err := json.Unmarshal(target.RecoveryData, &plan); err != nil {
				return schedulertypes.Outcome{}, err
			}
			planned = true
			break
		}
	}

	dockerClient, err := j.getDockerClientInternal(ctx)
	if err != nil {
		return schedulertypes.Outcome{}, err
	}
	message, err := j.confirmRestartTargetsInternal(ctx, dockerClient, previous.Outcome.Targets)
	if err != nil {
		return schedulertypes.Outcome{}, err
	}
	if message != "" {
		return schedulertypes.Outcome{Status: schedulertypes.NeedsAttention, Message: message, Targets: previous.Outcome.Targets}, nil
	}
	if !planned {
		return schedulertypes.Outcome{Status: schedulertypes.Succeeded, Targets: previous.Outcome.Targets}, nil
	}
	return j.resumeHealingPlanInternal(ctx, dockerClient, plan, previous)
}

func (j *AutoHealJob) confirmRestartTargetsInternal(ctx context.Context, dockerClient *client.Client, targets []schedulertypes.TargetOutcome) (string, error) {
	message := ""
	for index, target := range targets {
		if target.Status == schedulertypes.Succeeded || target.Status == schedulertypes.Skipped {
			continue
		}
		var baseline autoHealBaselineInternal
		if len(target.RecoveryData) == 0 || json.Unmarshal(target.RecoveryData, &baseline) != nil {
			message = "A container restart has no persisted baseline"
			break
		}
		inspect, inspectErr := j.inspectContainerInternal(ctx, dockerClient, target.ID)
		if inspectErr != nil || inspect.State == nil || inspect.State.Restarting || (inspect.State.StartedAt == baseline.StartedAt && inspect.RestartCount == baseline.RestartCount) {
			message = "A container restart has an unconfirmed outcome"
			break
		}
		target.Status = schedulertypes.Succeeded
		target.Message = "Container restart confirmed after recovery"
		if err := jobcontext.Progress(ctx, target); err != nil {
			return "", err
		}
		targets[index] = target
	}
	return message, nil
}

func (j *AutoHealJob) resumeHealingPlanInternal(ctx context.Context, dockerClient *client.Client, plan autoHealPlanInternal, previous schedulertypes.Run) (schedulertypes.Outcome, error) {
	outcome := schedulertypes.Outcome{Status: schedulertypes.Succeeded}
	for _, candidate := range plan.Containers {
		attempted := false
		for _, target := range previous.Outcome.Targets {
			if target.ID == candidate.ID {
				attempted = true
				break
			}
		}
		if attempted {
			continue
		}
		target, err := j.processCandidateInternal(ctx, dockerClient, candidate, plan.MaxRestarts, time.Duration(plan.WindowMinutes)*time.Minute, plan.WindowMinutes)
		if progressErr := jobcontext.Progress(ctx, target); progressErr != nil {
			return schedulertypes.Outcome{}, progressErr
		}
		previous.Outcome.Targets = append(previous.Outcome.Targets, target)
		if err != nil {
			outcome.Status = schedulertypes.NeedsAttention
			outcome.Message = err.Error()
			break
		}
	}
	outcome.Targets = previous.Outcome.Targets
	return outcome, nil
}
