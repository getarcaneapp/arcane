package edge

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// ErrNoActiveAgentTunnel is returned when no active agent tunnel exists for outbound event sync.
var ErrNoActiveAgentTunnel = errors.New("no active edge agent tunnel")

var agentTunnelState struct {
	mu   sync.RWMutex
	conn TunnelConnection
}

func setActiveAgentTunnelConn(conn TunnelConnection) {
	agentTunnelState.mu.Lock()
	defer agentTunnelState.mu.Unlock()
	agentTunnelState.conn = conn
}

func clearActiveAgentTunnelConn(conn TunnelConnection) {
	agentTunnelState.mu.Lock()
	defer agentTunnelState.mu.Unlock()
	if agentTunnelState.conn == conn {
		agentTunnelState.conn = nil
	}
}

func getActiveAgentTunnelConn() TunnelConnection {
	agentTunnelState.mu.RLock()
	defer agentTunnelState.mu.RUnlock()
	return agentTunnelState.conn
}

// PublishEventToManager sends an event from the active agent tunnel to the manager.
func PublishEventToManager(event *TunnelEvent) error {
	if event == nil {
		return fmt.Errorf("event is required")
	}
	if strings.TrimSpace(event.Type) == "" {
		return fmt.Errorf("event type is required")
	}
	if strings.TrimSpace(event.Title) == "" {
		return fmt.Errorf("event title is required")
	}

	conn := getActiveAgentTunnelConn()
	if conn == nil || conn.IsClosed() {
		return ErrNoActiveAgentTunnel
	}

	msg := &TunnelMessage{
		ID:    uuid.NewString(),
		Type:  MessageTypeEvent,
		Event: cloneTunnelEvent(event),
	}
	return conn.Send(msg)
}

func cloneTunnelEvent(event *TunnelEvent) *TunnelEvent {
	if event == nil {
		return nil
	}
	cloned := *event
	if len(event.MetadataJSON) > 0 {
		cloned.MetadataJSON = append([]byte(nil), event.MetadataJSON...)
	}
	return &cloned
}
