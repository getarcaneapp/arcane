package edge

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/cenkalti/backoff/v5"
)

const defaultTunnelPollRequestTimeout = 15 * time.Second

var defaultPollManagedSessionStopTimeout = 5 * time.Second

func (c *TunnelClient) connectAndServePoll(ctx context.Context) error {
	managerBaseURL := strings.TrimRight(strings.TrimSpace(c.cfg.GetManagerBaseURL()), "/")
	if managerBaseURL == "" {
		return errors.New("manager base URL is empty")
	}
	httpClient, err := NewManagerHTTPClient(c.cfg, 0)
	if err != nil {
		return errors.WrapIf(err, "failed to configure edge poll client")
	}
	c.httpClient = httpClient

	pollURL := managerBaseURL + "/api/tunnel/poll"
	interval := DefaultTunnelPollInterval
	// Failed polls back off exponentially instead of retrying every 2s forever;
	// a successful poll restores the manager-advertised interval. No jitter: the
	// default randomization lets the very first retry exceed the advertised
	// interval by up to 50%, and poll cycles are already phase-spread across
	// agents (each notices an outage at its own next tick), so there is no
	// synchronized herd for jitter to break up. The first retry is therefore
	// deterministically the poll interval itself.
	retryBackoff := backoff.NewExponentialBackOff()
	retryBackoff.InitialInterval = interval
	retryBackoff.RandomizationFactor = 0
	retryBackoff.MaxInterval = maxPollRetryInterval
	var session *pollManagedTunnelSession
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultPollManagedSessionStopTimeout)
		defer cancel()

		if err := c.stopPollManagedSessionInternal(shutdownCtx, session); err != nil {
			slog.WarnContext(ctx, "Failed to stop poll-managed websocket session during shutdown", "error", err)
		}
	}()

	for {
		var err error
		session, err = consumePollManagedSessionInternal(session)
		if err != nil {
			return err
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		status, err := c.pollTunnelControlInternal(ctx, pollURL, session != nil)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			retryInterval := retryBackoff.NextBackOff()
			slog.WarnContext(ctx, "Poll control request failed, retrying after interval",
				"error", err,
				"interval", retryInterval,
			)

			session, err = waitForNextPollCycleInternal(ctx, session, retryInterval)
			if err != nil {
				return err
			}
			continue
		}

		if status.PollIntervalSeconds > 0 {
			interval = time.Duration(status.PollIntervalSeconds) * time.Second
		}
		// Reset copies InitialInterval into the current interval, so the
		// manager-advertised value must be applied before resetting.
		retryBackoff.InitialInterval = interval
		retryBackoff.Reset()

		session, err = c.syncPollManagedSessionInternal(ctx, session, status.Status)
		if err != nil {
			return err
		}

		session, err = waitForNextPollCycleInternal(ctx, session, interval)
		if err != nil {
			return err
		}
	}
}

func consumePollManagedSessionInternal(session *pollManagedTunnelSession) (*pollManagedTunnelSession, error) {
	if session == nil {
		return nil, nil
	}

	select {
	case err := <-session.done:
		if err != nil {
			return nil, errors.WrapIf(err, "poll-managed websocket session ended")
		}
		return nil, nil
	default:
		return session, nil
	}
}

func (c *TunnelClient) syncPollManagedSessionInternal(ctx context.Context, session *pollManagedTunnelSession, status string) (*pollManagedTunnelSession, error) {
	switch status {
	case TunnelStatusRequired, TunnelStatusActive:
		if session != nil {
			return session, nil
		}

		slog.InfoContext(ctx, "Poll control plane requested edge tunnel",
			"status", status,
			"manager_grpc_addr", c.managerGRPCAddr,
			"manager_ws_url", c.managerWebSocketURLInternal(),
		)
		return c.startPollManagedSessionInternal(ctx), nil
	case TunnelStatusIdle:
		if session == nil {
			return nil, nil
		}

		slog.InfoContext(ctx, "Poll control plane marked edge tunnel idle; closing session")
		idleCtx, idleCancel := context.WithTimeout(ctx, defaultPollManagedSessionStopTimeout)
		err := c.stopPollManagedSessionInternal(idleCtx, session)
		idleCancel()
		if err != nil {
			return nil, err
		}
		return nil, nil
	default:
		slog.WarnContext(ctx, "Received unknown poll control status", "status", status)
		return session, nil
	}
}

func waitForNextPollCycleInternal(ctx context.Context, session *pollManagedTunnelSession, interval time.Duration) (*pollManagedTunnelSession, error) {
	waitTimer := time.NewTimer(interval)
	defer waitTimer.Stop()

	var sessionDone <-chan error
	if session != nil {
		sessionDone = session.done
	}

	select {
	case <-ctx.Done():
		return session, ctx.Err()
	case err := <-sessionDone:
		if err != nil {
			return nil, errors.WrapIf(err, "poll-managed websocket session ended")
		}
		return nil, nil
	case <-waitTimer.C:
		return session, nil
	}
}

func (c *TunnelClient) pollTunnelControlInternal(ctx context.Context, pollURL string, connected bool) (*TunnelPollResponse, error) {
	pollReq := TunnelPollRequest{
		Transport: EdgeTransportPoll,
		Connected: connected,
	}

	body, err := json.Marshal(pollReq)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to marshal poll request")
	}

	reqCtx, cancel := context.WithTimeout(ctx, defaultTunnelPollRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, pollURL, bytes.NewReader(body))
	if err != nil {
		return nil, errors.WrapIf(err, "failed to create poll request")
	}
	req.Header.Set("Content-Type", "application/json")
	for header, value := range agentAuthCredentialsInternal(c.cfg.AgentToken) {
		req.Header.Set(header, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.WrapIf(err, "poll request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, errors.Errorf("poll request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var pollResp TunnelPollResponse
	if err := json.UnmarshalRead(resp.Body, &pollResp); err != nil {
		return nil, errors.WrapIf(err, "failed to decode poll response")
	}
	return &pollResp, nil
}

func (c *TunnelClient) startPollManagedSessionInternal(ctx context.Context) *pollManagedTunnelSession {
	sessionCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)

	go func() {
		err := c.connectAndServeManagedTunnelInternal(sessionCtx)
		if sessionCtx.Err() != nil {
			err = nil
		}
		done <- err
		close(done)
	}()

	return &pollManagedTunnelSession{cancel: cancel, done: done}
}

func (c *TunnelClient) stopPollManagedSessionInternal(ctx context.Context, session *pollManagedTunnelSession) error {
	if session == nil {
		return nil
	}
	session.cancel()

	select {
	case err := <-session.done:
		return err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("timed out waiting for poll-managed websocket session to stop")
		}
		return ctx.Err()
	}
}
