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

	kit "go.getarcane.app/kit/pkg"
)

const (
	// TunnelEventTypeNotificationDispatch marks an event carrying a notification dispatch request in MetadataJSON;
	// tunnel-connected agents lack manager HTTP config, so dispatch rides the event channel (#3002).
	TunnelEventTypeNotificationDispatch = "notification.dispatch"

	// agentEventTimeout bounds the manager's processing of one agent event.
	agentEventTimeout = 15 * time.Second
)

// ErrNoActiveAgentTunnel is returned when no active agent tunnel exists for outbound event sync.
var ErrNoActiveAgentTunnel = errors.New("no active agent tunnel")

// activeAgentEvents is the live multiplexed session's client for posting events to the manager.
var activeAgentEvents atomic.Pointer[http.Client]

// PublishEventToManager sends an event from the active agent tunnel to the manager.
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

// validateTunnelEvent checks the fields both peers require of an agent event.
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
