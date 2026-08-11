package edge

import (
	"strings"
	"sync"

	"emperror.dev/errors"

	"github.com/google/uuid"
)

const (
	// ErrNoActiveAgentTunnel is returned when no active agent tunnel exists for outbound event sync.
	ErrNoActiveAgentTunnel = errors.Sentinel("no active edge agent tunnel")

	// TunnelEventTypeNotificationDispatch marks a tunnel event that carries a
	// notification dispatch request in MetadataJSON instead of an event-log entry.
	// Tunnel-connected agents have no manager HTTP config, so notification
	// dispatch rides the existing agent-to-manager event channel (#3002).
	TunnelEventTypeNotificationDispatch = "notification.dispatch"
)

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
		return errors.New("event is required")
	}
	if strings.TrimSpace(event.Type) == "" {
		return errors.New("event type is required")
	}
	if strings.TrimSpace(event.Title) == "" {
		return errors.New("event title is required")
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
