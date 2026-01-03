package job

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	backoff "github.com/cenkalti/backoff/v5"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/go-co-op/gocron/v2"
)

const (
	AnalyticsJobName         = "analytics-heartbeat"
	defaultHeartbeatEndpoint = "https://checkin.getarcane.app/heartbeat"
	analyticsInterval        = 24 * time.Hour
)

type AnalyticsJob struct {
	scheduler       *Scheduler
	settingsService *services.SettingsService
	httpClient      *http.Client
	heartbeatURL    string
	cfg             *config.Config
}

func NewAnalyticsJob(
	scheduler *Scheduler,
	settingsService *services.SettingsService,
	httpClient *http.Client,
	cfg *config.Config,
) *AnalyticsJob {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &AnalyticsJob{
		scheduler:       scheduler,
		settingsService: settingsService,
		httpClient:      httpClient,
		heartbeatURL:    defaultHeartbeatEndpoint,
		cfg:             cfg,
	}
}

func (j *AnalyticsJob) Register(ctx context.Context) error {
	if j.cfg.AnalyticsDisabled || !j.cfg.Environment.IsProdEnvironment() {
		slog.InfoContext(ctx, "analytics disabled or not in production; heartbeat job not registered", "analyticsDisabled", j.cfg.AnalyticsDisabled, "env", j.cfg.Environment)
		return nil
	}

	j.scheduler.RemoveJobByName(AnalyticsJobName)

	jobDefinition := gocron.DurationJob(analyticsInterval)
	slog.InfoContext(ctx, "registering analytics heartbeat job", "jobName", AnalyticsJobName, "interval", analyticsInterval.String(), "endpoint", j.heartbeatURL)

	return j.scheduler.RegisterJob(
		ctx,
		AnalyticsJobName,
		jobDefinition,
		j.Execute,
		true, // run immediately on startup
	)
}

func (j *AnalyticsJob) Execute(parentCtx context.Context) error {
	if j.cfg.AnalyticsDisabled || !j.cfg.Environment.IsProdEnvironment() {
		slog.InfoContext(parentCtx, "analytics disabled or not in production; skipping heartbeat", "analyticsDisabled", j.cfg.AnalyticsDisabled, "env", j.cfg.Environment)
		return nil
	}

	instanceID := j.settingsService.GetStringSetting(parentCtx, "instanceId", "")

	payload := struct {
		Version    string `json:"version"`
		InstanceID string `json:"instance_id"`
	}{
		Version:    getVersion(),
		InstanceID: instanceID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal analytics heartbeat body: %w", err)
	}

	slog.InfoContext(parentCtx, "sending analytics heartbeat", "jobName", AnalyticsJobName)

	_, err = backoff.Retry(
		parentCtx,
		func() (struct{}, error) {
			ctx, cancel := context.WithTimeout(parentCtx, 20*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.heartbeatURL, bytes.NewReader(body))
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
		slog.ErrorContext(parentCtx, "analytics heartbeat failed", "error", err)
		return fmt.Errorf("analytics heartbeat request failed: %w", err)
	}

	slog.InfoContext(parentCtx, "analytics heartbeat sent successfully", "jobName", AnalyticsJobName)
	return nil
}

func getVersion() string {
	if v := strings.TrimSpace(config.Version); v != "" && v != "dev" {
		return v
	}
	return "unknown"
}
