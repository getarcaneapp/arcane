package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	backoff "github.com/cenkalti/backoff/v5"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/services"
)

const (
	AnalyticsJobName                 = "analytics-heartbeat"
	defaultHeartbeatEndpoint         = "https://checkin.getarcane.app/heartbeat"
	devHeartbeatEndpoint             = "http://localhost:8080/heartbeat"
	analyticsHeartbeatLastAttemptKey = "analytics.heartbeat.last_attempt_at"
	analyticsHeartbeatDedupeWindow   = 24 * time.Hour
	analyticsHeartbeatCheckSchedule  = "0 0 * * * *"
)

type AnalyticsJob struct {
	settingsService *services.SettingsService
	kvService       *services.KVService
	httpClient      *http.Client
	heartbeatURL    string
	cfg             *config.Config
	runMu           sync.Mutex
	now             func() time.Time
}

func NewAnalyticsJob(
	settingsService *services.SettingsService,
	kvService *services.KVService,
	httpClient *http.Client,
	cfg *config.Config,
) *AnalyticsJob {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	heartbeatURL := defaultHeartbeatEndpoint
	if !cfg.Environment.IsProdEnvironment() {
		heartbeatURL = devHeartbeatEndpoint
	}
	return &AnalyticsJob{
		settingsService: settingsService,
		kvService:       kvService,
		httpClient:      httpClient,
		heartbeatURL:    heartbeatURL,
		cfg:             cfg,
		now:             time.Now,
	}
}

func (j *AnalyticsJob) Name() string {
	return AnalyticsJobName
}

func (j *AnalyticsJob) Schedule(_ context.Context) string {
	return analyticsHeartbeatCheckSchedule
}

func (j *AnalyticsJob) Run(ctx context.Context) {
	if j.cfg.AnalyticsDisabled {
		slog.DebugContext(ctx, "analytics disabled; skipping heartbeat", "analyticsDisabled", j.cfg.AnalyticsDisabled)
		return
	}
	if j.cfg.Environment.IsTestEnvironment() {
		slog.DebugContext(ctx, "test environment; skipping heartbeat", "env", j.cfg.Environment)
		return
	}

	allowed, err := j.claimHeartbeatAttemptWindowInternal(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to acquire analytics heartbeat send window", "error", err)
		return
	}
	if !allowed {
		return
	}

	instanceID := j.settingsService.GetStringSetting(ctx, "instanceId", "")

	payload := struct {
		Version    string `json:"version"`
		InstanceID string `json:"instance_id"`
		ServerType string `json:"server_type,omitempty"`
	}{
		Version:    getAnalyticsVersion(),
		InstanceID: instanceID,
		ServerType: j.getServerType(),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.ErrorContext(ctx, "failed to marshal analytics heartbeat body", "error", err)
		return
	}

	slog.InfoContext(
		ctx,
		"sending analytics heartbeat",
		"jobName",
		AnalyticsJobName,
		"version",
		payload.Version,
		"instanceID",
		payload.InstanceID,
		"serverType",
		payload.ServerType,
		"heartbeatURL",
		j.heartbeatURL,
		"env",
		j.cfg.Environment,
	)

	_, err = backoff.Retry(
		ctx,
		func() (struct{}, error) {
			reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, j.heartbeatURL, bytes.NewReader(body))
			if err != nil {
				return struct{}{}, fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := j.httpClient.Do(req) //nolint:gosec // intentional request to configured analytics heartbeat endpoint
			if err != nil {
				return struct{}{}, fmt.Errorf("failed to send request: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
				bodyText := strings.TrimSpace(string(respBody))
				retryAfter := strings.TrimSpace(resp.Header.Get("Retry-After"))

				reason := resp.Status
				if resp.StatusCode == http.StatusTooManyRequests {
					reason = "rate limited by analytics heartbeat endpoint (429 Too Many Requests)"
				}

				details := []string{reason}
				if retryAfter != "" {
					details = append(details, "retry-after="+retryAfter)
				}
				if bodyText != "" {
					details = append(details, "response="+bodyText)
				}
				return struct{}{}, fmt.Errorf("analytics heartbeat request failed: %s", strings.Join(details, "; "))
			}
			return struct{}{}, nil
		},
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
		backoff.WithMaxTries(3),
	)
	if err != nil {
		slog.ErrorContext(ctx, "analytics heartbeat failed", "error", err)
		return
	}

	slog.InfoContext(
		ctx,
		"analytics heartbeat sent successfully",
		"jobName",
		AnalyticsJobName,
		"version",
		payload.Version,
		"instanceID",
		payload.InstanceID,
		"serverType",
		payload.ServerType,
		"heartbeatURL",
		j.heartbeatURL,
		"env",
		j.cfg.Environment,
	)
}

func (j *AnalyticsJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "analytics heartbeat schedule is fixed and managed internally", "schedule", analyticsHeartbeatCheckSchedule)
	return nil
}

func (j *AnalyticsJob) claimHeartbeatAttemptWindowInternal(ctx context.Context) (bool, error) {
	if j.kvService == nil {
		return false, fmt.Errorf("analytics heartbeat kv service is not configured")
	}

	j.runMu.Lock()
	defer j.runMu.Unlock()

	now := j.now().UTC()
	rawLastAttemptAt, ok, err := j.kvService.Get(ctx, analyticsHeartbeatLastAttemptKey)
	if err != nil {
		return false, fmt.Errorf("failed to load analytics heartbeat attempt state: %w", err)
	}

	if ok {
		lastAttemptAt, parseErr := time.Parse(time.RFC3339Nano, rawLastAttemptAt)
		if parseErr != nil {
			slog.WarnContext(
				ctx,
				"invalid analytics heartbeat attempt timestamp; resetting dedupe window",
				"key",
				analyticsHeartbeatLastAttemptKey,
				"value",
				rawLastAttemptAt,
				"error",
				parseErr,
			)
		} else {
			nextEligibleAt := lastAttemptAt.Add(analyticsHeartbeatDedupeWindow)
			if now.Before(nextEligibleAt) {
				slog.InfoContext(
					ctx,
					"skipping analytics heartbeat; already attempted within dedupe window",
					"jobName",
					AnalyticsJobName,
					"lastAttemptAt",
					lastAttemptAt,
					"nextEligibleAt",
					nextEligibleAt,
				)
				return false, nil
			}
		}
	}

	// Persist the attempt window before sending on purpose: the product behavior is
	// best-effort at-most-once-per-24h check-ins, which avoids duplicate heartbeats
	// after restarts or partially completed outbound requests.
	if err := j.kvService.Set(ctx, analyticsHeartbeatLastAttemptKey, now.Format(time.RFC3339Nano)); err != nil {
		return false, fmt.Errorf("failed to persist analytics heartbeat attempt state: %w", err)
	}

	return true, nil
}

func (j *AnalyticsJob) getServerType() string {
	if j.cfg.AgentMode {
		return "agent"
	}
	return "manager"
}

func getAnalyticsVersion() string {
	if v := strings.TrimSpace(config.Version); v != "" && v != "dev" {
		return v
	}
	return "unknown"
}
