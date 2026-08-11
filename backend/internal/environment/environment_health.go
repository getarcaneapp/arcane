package environment

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/types/v2/environment"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/moby/moby/client"
)

const (
	defaultEnvironmentHealthInterval = "0 */2 * * * *"
	environmentHealthCheckTimeout    = 90 * time.Second
)

const environmentHealthJobPrefix = "environment-health:"

const environmentHealthAdmissionScopeInternal = "environment-health"

// SetScheduler injects the job scheduler and app lifecycle context. Called during
// bootstrap on the manager only (agent mode leaves scheduler nil, so all health-job
// registration becomes a no-op).
func (s *EnvironmentService) SetScheduler(ctx context.Context, scheduler schedulertypes.DynamicScheduler, admissionGate *actors.Gate[actors.AdmissionKey]) error {
	return s.jobs.SetScheduler(ctx, scheduler, admissionGate)
}

// registerHealthJobInternal schedules one environment's health check on the
// global environmentHealthInterval (environment health has no per-entity
// interval); the run body self-cancels cleanly when the environment is gone.
func (s *EnvironmentService) registerHealthJobInternal(ctx context.Context, envID string) {
	s.jobs.Register(ctx, envID,
		func(ctx context.Context) string {
			sched := s.settingsService.GetStringSetting(ctx, "environmentHealthInterval", defaultEnvironmentHealthInterval)
			if sched == "" {
				return defaultEnvironmentHealthInterval
			}
			return sched
		},
		func(ctx context.Context) { s.runHealthCheckInternal(ctx, envID) },
	)
}

func (s *EnvironmentService) removeHealthJobInternal(ctx context.Context, envID string) {
	s.jobs.Unregister(ctx, envID)
}

func (s *EnvironmentService) ListEnabledEnvironmentIDs(ctx context.Context) ([]string, error) {
	var ids []string
	if err := s.db.WithContext(ctx).
		Table("environments").
		Where("enabled = ?", true).
		Pluck("id", &ids).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to list enabled environments")
	}
	return ids, nil
}

func (s *EnvironmentService) registerAllEnabledHealthJobsInternal(ctx context.Context) int {
	ids, err := s.ListEnabledEnvironmentIDs(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list environments for health jobs", "error", err)
		return 0
	}
	for _, id := range ids {
		s.registerHealthJobInternal(ctx, id)
	}
	return len(ids)
}

// RegisterHealthJobsOnStartup registers a health-check job for every enabled
// environment. Replaces the old global environment-health job.
func (s *EnvironmentService) RegisterHealthJobsOnStartup(ctx context.Context) {
	if !s.jobs.Enabled() {
		return
	}
	n := s.registerAllEnabledHealthJobsInternal(ctx)
	slog.InfoContext(ctx, "Registered environment health jobs on startup", "count", n)
}

// RescheduleHealthJobs re-registers all enabled environments' health jobs, picking
// up a changed global interval. Wired from the Jobs UI via job.JobService.
func (s *EnvironmentService) RescheduleHealthJobs(ctx context.Context) {
	if !s.jobs.Enabled() {
		return
	}
	s.registerAllEnabledHealthJobsInternal(ctx)
}

// RunHealthChecksNow runs every enabled environment's health check synchronously.
// Backs the "run now" button for the environment-health job in the Jobs UI.
func (s *EnvironmentService) RunHealthChecksNow(ctx context.Context) error {
	ids, err := s.ListEnabledEnvironmentIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		s.runHealthCheckInternal(ctx, id)
	}
	return nil
}

// runHealthCheckInternal tests one environment's connection (updating its DB status)
// and, for online remotes, syncs registries and repositories to it.
func (s *EnvironmentService) runHealthCheckInternal(ctx context.Context, envID string) {
	lease, admitted, err := s.jobs.TryAcquire(ctx, envID)
	if err != nil {
		slog.ErrorContext(ctx, "environment health check admission failed", "environment_id", envID, "error", err)
		return
	}
	if !admitted {
		slog.WarnContext(ctx, "environment health check skipped; previous run still in progress", "environment_id", envID)
		return
	}
	defer lease.Release()

	status, err := s.TestConnection(ctx, envID, nil)
	switch {
	case err != nil:
		slog.WarnContext(ctx, "environment health check failed", "environment_id", envID, "status", status, "error", err)
		return
	case status != "online":
		return
	}

	// Local environment (ID "0") has no registries/repositories to push.
	if envID == "0" {
		return
	}

	syncCtx, cancel := context.WithTimeout(ctx, environmentHealthCheckTimeout)
	defer cancel()
	if err := s.SyncRegistriesToEnvironment(syncCtx, envID); err != nil {
		slog.WarnContext(syncCtx, "failed to sync registries during health check", "environment_id", envID, "error", err)
	}
	if err := s.SyncS3DestinationsToEnvironment(syncCtx, envID); err != nil {
		slog.WarnContext(syncCtx, "failed to sync S3 destinations during health check", "environment_id", envID, "error", err)
	}
	if err := s.SyncRepositoriesToEnvironment(syncCtx, envID); err != nil {
		slog.WarnContext(syncCtx, "failed to sync git repositories during health check", "environment_id", envID, "error", err)
	}
	if s.variableSyncer != nil {
		if err := s.variableSyncer.SyncEnvironment(syncCtx, envID); err != nil {
			slog.WarnContext(syncCtx, "failed to sync global variables during health check", "environment_id", envID, "error", err)
		}
	}
}

func (s *EnvironmentService) TestConnection(ctx context.Context, id string, customApiUrl *string) (string, error) {
	envRecord, err := s.GetEnvironmentByID(ctx, id)
	if err != nil {
		return "error", err
	}
	connectionFailure := func(status string, failure error) (string, error) {
		if customApiUrl == nil {
			return status, failure
		}
		slog.WarnContext(ctx, "Environment custom URL connection test failed", "environment_id", id, "error", failure)
		return "error", common.ErrEnvironmentConnectionTestFailed
	}

	// Special handling for local Docker environment (ID "0")
	if id == "0" && customApiUrl == nil {
		return s.testLocalDockerConnection(ctx, id)
	}

	// For edge environments, check if there's an active tunnel and route through it
	if envRecord.IsEdge && customApiUrl == nil {
		return s.testEdgeConnection(ctx, id)
	}

	apiUrl := envRecord.ApiUrl
	if customApiUrl != nil {
		apiUrl = *customApiUrl
	}

	healthURL, err := buildEnvironmentEndpointURLInternal(apiUrl, "/api/health")
	if err != nil {
		if customApiUrl == nil {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		}
		return connectionFailure("offline", errors.WrapIf(err, "invalid environment API URL"))
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, healthURL, nil)
	if err != nil {
		if customApiUrl == nil {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		}
		return connectionFailure("offline", errors.WrapIf(err, "failed to create request"))
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		if customApiUrl == nil {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		}
		return connectionFailure("offline", errors.WrapIf(err, "connection failed"))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		if customApiUrl == nil {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOnline))
		}
		return "online", nil
	}

	if customApiUrl == nil {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusError))
	}
	return connectionFailure("error", errors.Errorf("unexpected status code: %d", resp.StatusCode))
}

// testEdgeConnection tests connection to an edge agent via its tunnel
func (s *EnvironmentService) testEdgeConnection(ctx context.Context, id string) (string, error) {
	// The health check itself is standing demand for the tunnel; refreshing here
	// keeps idle poll-mode tunnels open across check cycles instead of letting
	// the demand TTL expire and the tunnel flap between checks.
	edge.TouchTunnelDemand(id, edge.DefaultTunnelDemandTTL)
	if !edge.HasActiveTunnel(id) {
		if _, ok := edge.RequestTunnelAndWait(ctx, id, edge.DefaultTunnelDemandTTL, edge.DefaultTunnelAcquireTimeout()).Get(); !ok {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
			return "offline", errors.New("edge agent is not connected")
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	statusCode, _, err := edge.DoRequest(reqCtx, id, http.MethodGet, "/api/health", nil)
	if err != nil {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		return "offline", errors.WrapIf(err, "health check via tunnel failed")
	}

	if statusCode == http.StatusOK {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOnline))
		return "online", nil
	}

	_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusError))
	return "error", errors.Errorf("unexpected status code: %d", statusCode)
}

func (s *EnvironmentService) testLocalDockerConnection(ctx context.Context, id string) (string, error) {
	// Test local Docker socket by pinging Docker
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		return "offline", errors.WrapIf(err, "failed to connect to Docker")
	}

	_, err = dockerClient.Ping(reqCtx, client.PingOptions{})
	if err != nil {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		return "offline", errors.WrapIf(err, "docker ping failed")
	}

	_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOnline))
	return "online", nil
}

func (s *EnvironmentService) updateEnvironmentStatusInternal(ctx context.Context, id, status string) error {
	var currentEnv models.Environment
	if err := s.db.WithContext(ctx).Select("status", "is_edge").Where("id = ?", id).First(&currentEnv).Error; err != nil {
		return errors.WrapIf(err, "failed to check environment status")
	}

	if currentEnv.Status == string(models.EnvironmentStatusPending) {
		// Edge envs must complete pairing via the agent's outbound tunnel — manager
		// can't dial them directly, so a manager-side reachability check means nothing.
		// Direct envs are reachable from the manager, so a successful health check IS
		// the pairing signal. Don't promote on offline/error ticks though, or a transient
		// blip during initial setup would flip the env out of pending.
		if currentEnv.IsEdge || status != string(models.EnvironmentStatusOnline) {
			slog.DebugContext(ctx, "skipping status update for pending environment", "environment_id", id)
			return nil
		}
		slog.InfoContext(ctx, "promoted pending direct environment to online via reachability check", "environment_id", id)
	}

	now := time.Now()
	updates := map[string]any{
		"status":     status,
		"last_seen":  &now,
		"updated_at": &now,
	}
	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return errors.WrapIf(err, "failed to update environment status")
	}
	// Direct environments have no tunnel callback, so this health-check write is
	// the only moment their liveness changes.
	s.NotifyRuntimeStateChanged()
	return nil
}

func (s *EnvironmentService) UpdateEnvironmentHeartbeat(ctx context.Context, id string) error {
	now := time.Now()

	// Use Exec with raw SQL for better performance
	// Only update if last_seen is NULL or older than 30 seconds to reduce write frequency
	result := s.db.WithContext(ctx).Exec(`
		UPDATE environments
		SET last_seen = ?, status = ?, updated_at = ?
		WHERE id = ?
		AND (last_seen IS NULL OR last_seen < ?)
	`, new(now), string(models.EnvironmentStatusOnline), new(now), id, now.Add(-30*time.Second))

	if result.Error != nil {
		return errors.WrapIf(result.Error, "failed to update environment heartbeat")
	}

	// The 30s throttle above doubles as the notify throttle: a no-op heartbeat
	// changed nothing worth waking a stream for.
	if result.RowsAffected > 0 {
		s.NotifyRuntimeStateChanged()
	}

	return nil
}

// UpdateEnvironmentConnectionState updates runtime connectivity status without creating
// a generic "environment updated" event. This is used for edge tunnel connect/disconnect.
func (s *EnvironmentService) UpdateEnvironmentConnectionState(ctx context.Context, id string, connected bool) error {
	now := time.Now()

	updates := map[string]any{
		"updated_at": &now,
	}
	if connected {
		updates["status"] = string(models.EnvironmentStatusOnline)
		updates["last_seen"] = &now
		// Remember the tunnel transport so the UI can keep showing it after
		// the tunnel drops or while the agent is poll-only.
		if state, ok := edge.GetTunnelRuntimeState(id).Get(); ok && state.Transport != "" {
			updates["last_edge_transport"] = state.Transport
		}
	} else {
		updates["status"] = string(models.EnvironmentStatusOffline)
	}

	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return errors.WrapIf(err, "failed to update environment connection state")
	}

	s.NotifyRuntimeStateChanged()

	return nil
}

// ApplyEnvironmentRuntimeState normalizes edge environment runtime status using
// in-memory tunnel and poll registries without mutating persisted state.
func ApplyEnvironmentRuntimeState(env *environment.Environment) {
	if env == nil || !env.IsEdge {
		return
	}

	connected := false
	env.Connected = &connected
	env.ConnectedAt = nil
	env.LastHeartbeat = nil
	env.LastPollAt = nil
	env.EdgeTransport = nil
	env.EdgeSecurityMode = nil
	env.EdgeSessionID = nil
	env.EdgeAgentInstance = nil
	env.EdgeCapabilities = nil

	if pollState, ok := edge.GetPollRuntimeRegistry().Get(env.ID, time.Now()).Get(); ok {
		env.LastPollAt = pollState.LastPollAt
	}

	if runtimeState, ok := edge.GetTunnelRuntimeState(env.ID).Get(); ok {
		connected = true
		env.Connected = &connected
		env.Status = string(models.EnvironmentStatusOnline)
		env.ConnectedAt = runtimeState.ConnectedAt
		env.LastHeartbeat = runtimeState.LastHeartbeat
		if runtimeState.SecurityMode != "" {
			env.EdgeSecurityMode = &runtimeState.SecurityMode
		}
		if runtimeState.SessionID != "" {
			env.EdgeSessionID = &runtimeState.SessionID
		}
		if runtimeState.AgentInstance != "" {
			env.EdgeAgentInstance = &runtimeState.AgentInstance
		}
		if len(runtimeState.Capabilities) > 0 {
			env.EdgeCapabilities = append([]string(nil), runtimeState.Capabilities...)
		}
		if transport, ok := edge.GetActiveTunnelTransport(env.ID).Get(); ok {
			env.EdgeTransport = &transport
		} else if runtimeState.Transport != "" {
			env.EdgeTransport = &runtimeState.Transport
		}
		return
	}

	if env.LastPollAt != nil {
		env.Status = string(models.EnvironmentStatusStandby)
		return
	}

	if env.Status != string(models.EnvironmentStatusPending) {
		env.Status = string(models.EnvironmentStatusOffline)
	}
}

// ReconcileEdgeStatusesOnStartup resets edge environments to offline when the manager starts.
// Live edge tunnels are process-local runtime state, so persisted "online" flags can be stale
// after a restart until agents reconnect. Pending environments are left untouched.
func (s *EnvironmentService) ReconcileEdgeStatusesOnStartup(ctx context.Context) error {
	result := s.db.WithContext(ctx).Model(&models.Environment{}).
		Where("is_edge = ?", true).
		Where("status <> ?", string(models.EnvironmentStatusPending)).
		Where("status <> ?", string(models.EnvironmentStatusOffline)).
		Updates(map[string]any{
			"status":     string(models.EnvironmentStatusOffline),
			"updated_at": new(time.Now()),
		})
	if result.Error != nil {
		return errors.WrapIf(result.Error, "failed to reconcile edge environment statuses")
	}

	if result.RowsAffected > 0 {
		slog.InfoContext(ctx, "Reconciled stale edge environment statuses on startup", "count", result.RowsAffected)
	}

	return nil
}
