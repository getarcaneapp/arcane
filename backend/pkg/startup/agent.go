package startup

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
	"time"

	"github.com/cenkalti/backoff/v5"

	"kit.getarcane.app"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/types/v3"
)

type (
	AgentTokenStore interface {
		Get(ctx context.Context, key string) (string, bool, error)
		Set(ctx context.Context, key, value string) error
	}

	EnrollAgentOptions struct {
		Config       *agent.Config
		Store        AgentTokenStore
		DescribeHost func(context.Context) (types.AgentEnrollRequest, error)
	}

	agentEnrollmentAttempt struct {
		config       *agent.Config
		describeHost func(context.Context) (types.AgentEnrollRequest, error)
		storedToken  string
		requestID    string
	}

	enrollmentRejectedError struct {
		detail string
		status int
	}
)

const (
	enrolledAgentTokenKey  = "agent.enrolledToken"
	enrollmentRequestIDKey = "agent.enrollmentRequestId"
)

func (e *enrollmentRejectedError) Error() string {
	return fmt.Sprintf("enrollment failed with status %d: %s", e.status, e.detail)
}

func EnrollAgent(ctx context.Context, opts EnrollAgentOptions) (string, error) {
	if !strings.HasPrefix(opts.Config.AgentToken, types.EnrollmentTokenPrefix) {
		return opts.Config.AgentToken, nil
	}
	stored := loadEnrolledAgentToken(ctx, opts.Store)
	requestID := loadEnrollmentRequestID(ctx, opts.Store)

	token, err := backoff.Retry(ctx, func() (string, error) {
		token, err := requestAgentEnrollment(ctx, agentEnrollmentAttempt{
			config:       opts.Config,
			storedToken:  stored,
			requestID:    requestID,
			describeHost: opts.DescribeHost,
		})
		if rejected, ok := errors.AsType[*enrollmentRejectedError](err); ok &&
			rejected.status < http.StatusInternalServerError && rejected.status != http.StatusRequestTimeout && rejected.status != http.StatusTooManyRequests {
			return "", backoff.Permanent(err)
		}
		return token, err
	},
		backoff.WithBackOff(&backoff.ExponentialBackOff{
			InitialInterval:     max(time.Duration(opts.Config.AgentReconnectInterval)*time.Second, time.Second),
			RandomizationFactor: backoff.DefaultRandomizationFactor,
			Multiplier:          backoff.DefaultMultiplier,
			MaxInterval:         5 * time.Minute,
		}),
		backoff.WithMaxElapsedTime(0),
		backoff.WithMaxTries(kit.Ternary[uint](stored != "", 1, 0)),
		backoff.WithNotify(func(err error, next time.Duration) {
			slog.WarnContext(ctx, "Agent enrollment failed; retrying", "error", err, "retryIn", next.String())
		}),
	)
	if err != nil && stored != "" {
		slog.WarnContext(ctx, "Agent enrollment failed; using the stored agent token", "error", err)
		return stored, nil
	}
	if err != nil {
		return "", fmt.Errorf("agent enrollment with %s was refused (%w); set ARCANE_AGENT_TOKEN to a valid enrollment token from the manager", opts.Config.MTLS.ManagerURL, err)
	}

	if token != stored {
		ciphertext, storeErr := crypto.Encrypt(token)
		if storeErr == nil {
			storeErr = opts.Store.Set(ctx, enrolledAgentTokenKey, ciphertext)
		}
		if storeErr != nil {
			slog.WarnContext(ctx, "Failed to store the enrolled agent token; the agent enrolls again on restart", "error", storeErr)
		}
		if discardErr := opts.Config.MTLS.ResetClient(ctx); discardErr != nil {
			slog.WarnContext(ctx, "Failed to discard the agent mTLS client certificate of the previous enrollment", "error", discardErr)
		}
		slog.InfoContext(ctx, "Enrolled agent with manager", "managerUrl", opts.Config.MTLS.ManagerURL)
	}
	return token, nil
}

func PairAgent(ctx context.Context, cfg *agent.Config) error {
	slog.InfoContext(ctx, "Agent mode detected with token, attempting auto-pairing", "managerUrl", cfg.MTLS.ManagerURL)

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, httpx.ManagerBaseURL(cfg.MTLS.ManagerURL)+"/api/environments/pair", http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create pairing request: %w", err)
	}
	req.Header.Set(agent.HeaderAPIKey, cfg.AgentToken)

	httpClient, err := cfg.MTLS.ManagerClient(ctx, agent.ManagerClientOptions{Timeout: 10 * time.Second})
	if err != nil {
		return fmt.Errorf("failed to configure agent pairing client: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pairing request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	switch {
	case resp.StatusCode == http.StatusOK:
		slog.InfoContext(ctx, "Successfully paired agent with manager", "managerUrl", cfg.MTLS.ManagerURL)
		return nil
	case resp.StatusCode == http.StatusBadRequest && strings.Contains(string(body), "not in pending status"):
		slog.InfoContext(ctx, "Agent already paired with manager", "managerUrl", cfg.MTLS.ManagerURL)
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		slog.WarnContext(ctx, "Manager did not recognize ARCANE_AGENT_TOKEN; set ARCANE_AGENT_TOKEN to a new enrollment token from the manager", "managerUrl", cfg.MTLS.ManagerURL)
		return nil
	}
	return fmt.Errorf("pairing failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func loadEnrolledAgentToken(ctx context.Context, store AgentTokenStore) string {
	encrypted, found, err := store.Get(ctx, enrolledAgentTokenKey)
	if err != nil {
		slog.WarnContext(ctx, "Failed to load the stored agent token; enrolling again", "error", err)
		return ""
	}
	if !found {
		return ""
	}
	token, err := crypto.Decrypt(encrypted)
	if err != nil {
		slog.WarnContext(ctx, "Failed to decrypt the stored agent token; enrolling again", "error", err)
		return ""
	}
	return token
}

func loadEnrollmentRequestID(ctx context.Context, store AgentTokenStore) string {
	stored, found, err := store.Get(ctx, enrollmentRequestIDKey)
	if err != nil {
		slog.WarnContext(ctx, "Failed to load the stored enrollment request ID; starting a new enrollment request", "error", err)
	}
	if found && stored != "" {
		return stored
	}
	requestID := kit.RandomString(43)
	if setErr := store.Set(ctx, enrollmentRequestIDKey, requestID); setErr != nil {
		slog.WarnContext(ctx, "Failed to store the enrollment request ID; a restart cannot recover a lost enrollment reply", "error", setErr)
	}
	return requestID
}

func requestAgentEnrollment(ctx context.Context, attempt agentEnrollmentAttempt) (string, error) {
	enrollRequest, err := attempt.describeHost(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to describe the agent host: %w", err)
	}
	body, err := json.Marshal(types.AgentEnrollRequest{
		Hostname:       enrollRequest.Hostname,
		SwarmNodeID:    enrollRequest.SwarmNodeID,
		SwarmClusterID: enrollRequest.SwarmClusterID,
		RequestID:      attempt.requestID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode enrollment request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpx.ManagerBaseURL(attempt.config.MTLS.ManagerURL)+"/api/agents/enroll", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create enrollment request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(agent.HeaderEnrollmentToken, attempt.config.AgentToken)
	if attempt.storedToken != "" {
		req.Header.Set(agent.HeaderAgentToken, attempt.storedToken)
	}

	httpClient, err := attempt.config.MTLS.ManagerClient(ctx, agent.ManagerClientOptions{Timeout: 30 * time.Second})
	if err != nil {
		return "", fmt.Errorf("failed to configure enrollment client: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("enrollment request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", &enrollmentRejectedError{
			detail: strings.TrimSpace(string(detail)),
			status: resp.StatusCode,
		}
	}

	var enrolled types.ApiResponse[types.AgentEnrollResponse]
	if decodeErr := json.UnmarshalRead(io.LimitReader(resp.Body, 64<<10), &enrolled); decodeErr != nil {
		return "", fmt.Errorf("failed to decode enrollment response: %w", decodeErr)
	}
	token := strings.TrimSpace(enrolled.Data.Token)
	if !enrolled.Success || token == "" {
		return "", errors.New("manager returned no agent token")
	}
	return token, nil
}
