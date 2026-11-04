package bootstrap

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

	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/kv"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/types/v3"
)

type enrollmentRejectedError struct {
	detail string
	status int
}

const enrolledAgentTokenKey = "agent.enrolledToken"

func (e *enrollmentRejectedError) Error() string {
	return fmt.Sprintf("enrollment failed with status %d: %s", e.status, e.detail)
}

func enrollSwarmAgent(ctx context.Context, cfg *config.Config, kvService *kv.KVService, localNodeID func(context.Context) (string, error)) string {
	stored := ""
	encrypted, found, loadErr := kvService.Get(ctx, enrolledAgentTokenKey)
	switch {
	case loadErr != nil:
		slog.WarnContext(ctx, "Failed to load the stored agent token; enrolling again", "error", loadErr)
	case found:
		if decrypted, decryptErr := crypto.Decrypt(encrypted); decryptErr == nil {
			stored = decrypted
		} else {
			slog.WarnContext(ctx, "Failed to decrypt the stored agent token; enrolling again", "error", decryptErr)
		}
	}

	token, err := backoff.Retry(ctx, func() (string, error) {
		token, err := requestSwarmAgentEnrollment(ctx, cfg, stored, localNodeID)
		// Fall back to the stored token only on a definitive refusal; an unreachable manager can't confirm the token still works.
		if rejected, ok := errors.AsType[*enrollmentRejectedError](err); ok && stored != "" &&
			rejected.status < http.StatusInternalServerError && rejected.status != http.StatusRequestTimeout && rejected.status != http.StatusTooManyRequests {
			return "", backoff.Permanent(err)
		}
		return token, err
	},
		backoff.WithBackOff(&backoff.ExponentialBackOff{
			InitialInterval:     max(time.Duration(cfg.AgentReconnectInterval)*time.Second, time.Second),
			RandomizationFactor: backoff.DefaultRandomizationFactor,
			Multiplier:          backoff.DefaultMultiplier,
			MaxInterval:         5 * time.Minute,
		}),
		backoff.WithMaxElapsedTime(0),
		backoff.WithNotify(func(err error, next time.Duration) {
			slog.WarnContext(ctx, "Swarm agent enrollment failed; retrying", "error", err, "retryIn", next.String())
		}),
	)
	if err != nil {
		if stored != "" {
			slog.WarnContext(ctx, "Manager refused swarm agent enrollment; using the stored agent token", "error", err)
		}
		return stored
	}

	if token != stored {
		ciphertext, storeErr := crypto.Encrypt(token)
		if storeErr == nil {
			storeErr = kvService.Set(ctx, enrolledAgentTokenKey, ciphertext)
		}
		if storeErr != nil {
			slog.WarnContext(ctx, "Failed to store the enrolled agent token; the agent enrolls again on restart", "error", storeErr)
		}
		if discardErr := agent.DiscardAgentMTLSClientCertificate(ctx, buildAgentRuntimeConfig(cfg, token, agent.AgentHostInfo{})); discardErr != nil {
			slog.WarnContext(ctx, "Failed to discard the agent mTLS client certificate of the previous enrollment", "error", discardErr)
		}
		slog.InfoContext(ctx, "Enrolled swarm node agent with manager", "managerUrl", cfg.ManagerApiUrl)
	}
	return token
}

func requestSwarmAgentEnrollment(ctx context.Context, cfg *config.Config, storedToken string, localNodeID func(context.Context) (string, error)) (string, error) {
	nodeID, err := localNodeID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to read the local swarm node ID: %w", err)
	}
	body, err := json.Marshal(types.NodeAgentEnrollRequest{NodeID: nodeID})
	if err != nil {
		return "", fmt.Errorf("failed to encode enrollment request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	enrollURL := strings.TrimRight(httpx.ManagerBaseURL(cfg.ManagerApiUrl), "/") + "/api/swarm/agents/enroll"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, enrollURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create enrollment request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.HeaderEnrollmentToken, cfg.AgentEnrollmentToken)
	if storedToken != "" {
		req.Header.Set(middleware.HeaderAgentToken, storedToken)
	}

	httpClient, err := agent.NewManagerHTTPClient(buildAgentRuntimeConfig(cfg, storedToken, agent.AgentHostInfo{}), 10*time.Second)
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
		return "", &enrollmentRejectedError{status: resp.StatusCode, detail: strings.TrimSpace(string(detail))}
	}

	var enrolled types.ApiResponse[types.NodeAgentEnrollResponse]
	if decodeErr := json.UnmarshalRead(io.LimitReader(resp.Body, 64<<10), &enrolled); decodeErr != nil {
		return "", fmt.Errorf("failed to decode enrollment response: %w", decodeErr)
	}
	token := strings.TrimSpace(enrolled.Data.Token)
	if !enrolled.Success || token == "" {
		return "", errors.New("manager returned no agent token")
	}
	return token, nil
}
