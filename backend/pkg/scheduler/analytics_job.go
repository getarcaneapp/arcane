package scheduler

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

const (
	AnalyticsJobName                 = "analytics-heartbeat"
	defaultHeartbeatEndpoint         = "https://checkin.getarcane.app/heartbeat"
	devHeartbeatEndpoint             = "http://localhost:8080/heartbeat"
	analyticsHeartbeatNextAttemptKey = "analytics.heartbeat.next_attempt_at"
	// analyticsHeartbeatLastAttemptKey is the window older releases kept; it is carried forward once.
	analyticsHeartbeatLastAttemptKey = "analytics.heartbeat.last_attempt_at"
	analyticsHeartbeatDedupeWindow   = 24 * time.Hour
	// analyticsHeartbeatRetryDelay spaces retries of a failed send, which the hourly check then picks up.
	analyticsHeartbeatRetryDelay    = time.Hour
	analyticsHeartbeatCheckSchedule = "0 0 * * * *"
)

type AnalyticsJob struct {
	settingsService *settings.SettingsService
	kvService       *kv.KVService
	httpClient      *http.Client
	heartbeatURL    string
	cfg             *config.Config
	runMu           sync.Mutex
	now             func() time.Time
}

func NewAnalyticsJob(
	settingsService *settings.SettingsService,
	kvService *kv.KVService,
	httpClient *http.Client,
	cfg *config.Config,
) *AnalyticsJob {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	heartbeatURL := kit.Ternary(!cfg.Environment.IsProdEnvironment(), devHeartbeatEndpoint, defaultHeartbeatEndpoint)
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

func (j *AnalyticsJob) Run(ctx context.Context) (schedulertypes.Outcome, error) {
	if j.cfg.AnalyticsDisabled {
		slog.DebugContext(ctx, "analytics disabled; skipping heartbeat", "analyticsDisabled", j.cfg.AnalyticsDisabled)
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
	}
	if j.cfg.Environment.IsTestEnvironment() {
		slog.DebugContext(ctx, "test environment; skipping heartbeat", "env", j.cfg.Environment)
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
	}

	allowed, err := j.claimHeartbeat(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to acquire analytics heartbeat send window", "error", err)
		return schedulertypes.Outcome{}, err
	}
	if !allowed {
		return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
	}

	instanceID := j.settingsService.GetStringSetting(ctx, "instanceId", "")

	payload := struct {
		Version    string `json:"version"`
		InstanceID string `json:"instance_id"`
		ServerType string `json:"server_type,omitempty"`
	}{
		Version:    getAnalyticsVersion(),
		InstanceID: instanceID,
		ServerType: kit.Ternary(j.cfg.AgentMode, "agent", "manager"),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.ErrorContext(ctx, "failed to marshal analytics heartbeat body", "error", err)
		return schedulertypes.Outcome{}, err
	}

	slog.InfoContext(ctx, "sending analytics heartbeat", "version", payload.Version, "instanceId", payload.InstanceID,
		"serverType", payload.ServerType, "heartbeatUrl", j.heartbeatURL, "env", j.cfg.Environment)

	_, err = backoff.Retry(
		ctx,
		func() (struct{}, error) {
			reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()

			req, newRequestWithContextErr := http.NewRequestWithContext(reqCtx, http.MethodPost, j.heartbeatURL, bytes.NewReader(body))
			if newRequestWithContextErr != nil {
				return struct{}{}, fmt.Errorf("failed to create request: %w", newRequestWithContextErr)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, newRequestWithContextErr := j.httpClient.Do(req)
			if newRequestWithContextErr != nil {
				return struct{}{}, fmt.Errorf("failed to send request: %w", newRequestWithContextErr)
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
				failure := fmt.Errorf("analytics heartbeat request failed: %s", strings.Join(details, "; "))
				// Retrying a rejection within seconds only spends more of the endpoint's limit.
				if resp.StatusCode < http.StatusInternalServerError {
					return struct{}{}, backoff.Permanent(failure)
				}
				return struct{}{}, failure
			}
			return struct{}{}, nil
		},
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
		backoff.WithMaxTries(3),
	)
	if err != nil {
		slog.ErrorContext(ctx, "analytics heartbeat failed; retrying after the retry delay", "error", err, "retryDelay", analyticsHeartbeatRetryDelay)
		retryAt := j.now().UTC().Add(analyticsHeartbeatRetryDelay).Format(time.RFC3339Nano)
		if retryErr := j.kvService.Set(ctx, analyticsHeartbeatNextAttemptKey, retryAt); retryErr != nil {
			slog.WarnContext(ctx, "failed to shorten analytics heartbeat window after a failed send", "error", retryErr)
		}
		return schedulertypes.Outcome{}, err
	}

	slog.InfoContext(ctx, "analytics heartbeat sent successfully", "version", payload.Version, "instanceId", payload.InstanceID, "serverType", payload.ServerType)
	return schedulertypes.Outcome{Status: schedulertypes.Succeeded}, nil
}

func (j *AnalyticsJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "analytics heartbeat schedule is fixed and managed internally", "schedule", analyticsHeartbeatCheckSchedule)
	return nil
}

// claimHeartbeat reports whether a heartbeat is due and, if so, schedules the next one a day out. Claiming before
// sending keeps restarts from sending twice; a failed send moves the next attempt up to the retry delay.
func (j *AnalyticsJob) claimHeartbeat(ctx context.Context) (bool, error) {
	if j.kvService == nil {
		return false, errors.New("analytics heartbeat kv service is not configured")
	}
	j.runMu.Lock()
	defer j.runMu.Unlock()

	now := j.now().UTC()
	rawNextAttemptAt, ok, err := j.kvService.Get(ctx, analyticsHeartbeatNextAttemptKey)
	if err != nil {
		return false, fmt.Errorf("failed to load analytics heartbeat attempt state: %w", err)
	}
	nextAttemptAt, parseErr := time.Parse(time.RFC3339Nano, rawNextAttemptAt)
	if !ok {
		// An upgrade from a release that stored the last attempt keeps that window instead of sending again.
		rawLastAttemptAt, hadLast, lastErr := j.kvService.Get(ctx, analyticsHeartbeatLastAttemptKey)
		if lastErr != nil {
			return false, fmt.Errorf("failed to load analytics heartbeat attempt state: %w", lastErr)
		}
		if lastAttemptAt, lastParseErr := time.Parse(time.RFC3339Nano, rawLastAttemptAt); hadLast && lastParseErr == nil {
			ok, parseErr, nextAttemptAt = true, nil, lastAttemptAt.Add(analyticsHeartbeatDedupeWindow)
		}
	}
	if ok && parseErr != nil {
		slog.WarnContext(ctx, "invalid analytics heartbeat attempt timestamp; resetting window", "value", rawNextAttemptAt, "error", parseErr)
	}
	if ok && parseErr == nil && now.Before(nextAttemptAt) {
		slog.InfoContext(ctx, "skipping analytics heartbeat; next one is not due yet", "nextAttemptAt", nextAttemptAt)
		return false, nil
	}
	if setErr := j.kvService.Set(ctx, analyticsHeartbeatNextAttemptKey, now.Add(analyticsHeartbeatDedupeWindow).Format(time.RFC3339Nano)); setErr != nil {
		return false, fmt.Errorf("failed to persist analytics heartbeat attempt state: %w", setErr)
	}
	return true, nil
}

func getAnalyticsVersion() string {
	if v := strings.TrimSpace(config.Version); v != "" && v != "dev" {
		return v
	}
	return "unknown"
}
