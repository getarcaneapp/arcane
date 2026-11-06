package agent

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"kit.getarcane.app"
)

const (
	TunnelEventTypeNotificationDispatch = "notification.dispatch"
	agentEventTimeout                   = 15 * time.Second
)

var (
	ErrNoActiveAgentTunnel = errors.New("no active agent tunnel")

	activeAgentEvents atomic.Pointer[http.Client]
)

func PublishEventToManager(ctx context.Context, event *TunnelEvent) error {
	if err := validateTunnelEvent(event); err != nil {
		return err
	}

	client := activeAgentEvents.Load()
	if client == nil {
		return ErrNoActiveAgentTunnel
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode agent event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, muxManagerBaseURL+muxEventsPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create agent event request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return kit.Ternary(errors.Is(err, ErrTunnelConnectionClosed), fmt.Errorf("send agent event: %w: %w", ErrNoActiveAgentTunnel, err), fmt.Errorf("send agent event: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		message, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return fmt.Errorf("manager rejected agent event with status %d (reading response body: %w)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("manager rejected agent event with status %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	return nil
}

func validateTunnelEvent(event *TunnelEvent) error {
	switch {
	case event == nil:
		return errors.New("event is required")
	case strings.TrimSpace(event.Type) == "":
		return errors.New("event type is required")
	case strings.TrimSpace(event.Title) == "":
		return errors.New("event title is required")
	}
	return nil
}
