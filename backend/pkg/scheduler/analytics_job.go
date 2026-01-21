package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	backoff "github.com/cenkalti/backoff/v5"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/services"
)

const (
	AnalyticsJobName         = "analytics-heartbeat"
	defaultHeartbeatEndpoint = "https://checkin.getarcane.app/heartbeat"
)

type AnalyticsJob struct {
	settingsService *services.SettingsService
	httpClient      *http.Client
	heartbeatURL    string
	cfg             *config.Config
}

func NewAnalyticsJob(
	settingsService *services.SettingsService,
	httpClient *http.Client,
	cfg *config.Config,
) *AnalyticsJob {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &AnalyticsJob{
		settingsService: settingsService,
		httpClient:      httpClient,
		heartbeatURL:    defaultHeartbeatEndpoint,
		cfg:             cfg,
	}
}

func (j *AnalyticsJob) Name() string {
	return AnalyticsJobName
}

func (j *AnalyticsJob) Schedule(ctx context.Context) string {
	s := j.settingsService.GetStringSetting(ctx, "analyticsHeartbeatInterval", "0 0 0 * * *")
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

func (j *AnalyticsJob) Run(ctx context.Context) {
	if j.cfg.AnalyticsDisabled || !j.cfg.Environment.IsProdEnvironment() {
		slog.DebugContext(ctx, "analytics disabled or not in production; skipping heartbeat", "analyticsDisabled", j.cfg.AnalyticsDisabled, "env", j.cfg.Environment)
		return
	}

	instanceID := j.settingsService.GetStringSetting(ctx, "instanceId", "")

	payload := struct {
		Version    string `json:"version"`
		InstanceID string `json:"instance_id"`
	}{
		Version:    getAnalyticsVersion(),
		InstanceID: instanceID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.ErrorContext(ctx, "failed to marshal analytics heartbeat body", "error", err)
		return
	}

	slog.InfoContext(ctx, "sending analytics heartbeat", "jobName", AnalyticsJobName)

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

			resp, err := j.httpClient.Do(req)
			if err != nil {
				return struct{}{}, fmt.Errorf("failed to send request: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return struct{}{}, fmt.Errorf("request failed with status: %d", resp.StatusCode)
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

	slog.InfoContext(ctx, "analytics heartbeat sent successfully", "jobName", AnalyticsJobName)
}

func (j *AnalyticsJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling analytics heartbeat job in new scheduler; currently requires restart")
	return nil
}

func getAnalyticsVersion() string {
	if v := strings.TrimSpace(config.Version); v != "" && v != "dev" {
		return v
	}
	return "unknown"
}
