package ws

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
)

// WebSocketMetrics tracks active WebSocket connections and their counts.
type WebSocketMetrics struct {
	projectLogsActive   atomic.Int64
	containerLogsActive atomic.Int64
	containerStats      atomic.Int64
	containerExec       atomic.Int64
	systemStats         atomic.Int64
	serviceLogsActive   atomic.Int64
	seq                 atomic.Uint64
	mu                  sync.RWMutex
	connections         map[string]systemtypes.WebSocketConnectionInfo
}

// NewWebSocketMetrics creates a new WebSocketMetrics instance.
func NewWebSocketMetrics() *WebSocketMetrics {
	return &WebSocketMetrics{
		connections: make(map[string]systemtypes.WebSocketConnectionInfo),
	}
}

// Snapshot returns a point-in-time copy of the active connection counts.
func (m *WebSocketMetrics) Snapshot() systemtypes.WebSocketMetricsSnapshot {
	return systemtypes.WebSocketMetricsSnapshot{
		ProjectLogsActive:   m.projectLogsActive.Load(),
		ContainerLogsActive: m.containerLogsActive.Load(),
		ContainerStats:      m.containerStats.Load(),
		ContainerExec:       m.containerExec.Load(),
		SystemStats:         m.systemStats.Load(),
		ServiceLogsActive:   m.serviceLogsActive.Load(),
	}
}

// Connections returns a snapshot of all tracked WebSocket connections.
func (m *WebSocketMetrics) Connections() []systemtypes.WebSocketConnectionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]systemtypes.WebSocketConnectionInfo, 0, len(m.connections))
	for _, info := range m.connections {
		result = append(result, info)
	}
	return result
}

// RegisterConnection adds a connection to the tracker and increments the
// appropriate kind counter. Returns the assigned connection ID.
func (m *WebSocketMetrics) RegisterConnection(info systemtypes.WebSocketConnectionInfo) string {
	if info.ID == "" {
		info.ID = "ws-" + strconv.FormatUint(m.seq.Add(1), 10)
	}
	if info.StartedAt.IsZero() {
		info.StartedAt = time.Now().UTC()
	}
	m.mu.Lock()
	m.connections[info.ID] = info
	m.mu.Unlock()
	m.applyDelta(info.Kind, 1)
	return info.ID
}

// UnregisterConnection removes a connection from the tracker and decrements
// the appropriate kind counter.
func (m *WebSocketMetrics) UnregisterConnection(id string) {
	if id == "" {
		return
	}
	var info systemtypes.WebSocketConnectionInfo
	m.mu.Lock()
	if existing, ok := m.connections[id]; ok {
		info = existing
		delete(m.connections, id)
	}
	m.mu.Unlock()
	if info.Kind != "" {
		m.applyDelta(info.Kind, -1)
	}
}

func (m *WebSocketMetrics) applyDelta(kind string, delta int64) {
	switch kind {
	case systemtypes.WSKindProjectLogs:
		m.projectLogsActive.Add(delta)
	case systemtypes.WSKindContainerLogs:
		m.containerLogsActive.Add(delta)
	case systemtypes.WSKindContainerStats:
		m.containerStats.Add(delta)
	case systemtypes.WSKindContainerExec:
		m.containerExec.Add(delta)
	case systemtypes.WSKindSystemStats:
		m.systemStats.Add(delta)
	case systemtypes.WSKindServiceLogs:
		m.serviceLogsActive.Add(delta)
	}
}
