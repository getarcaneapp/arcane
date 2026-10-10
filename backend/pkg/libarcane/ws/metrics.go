package ws

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// WebSocketMetrics tracks active WebSocket connections and their counts.
type WebSocketMetrics struct {
	counters    map[string]*atomic.Int64
	seq         atomic.Uint64
	mu          sync.RWMutex
	connections map[string]systemtypes.WebSocketConnectionInfo
}

// NewWebSocketMetrics creates a new WebSocketMetrics instance.
func NewWebSocketMetrics() *WebSocketMetrics {
	return &WebSocketMetrics{
		counters: map[string]*atomic.Int64{
			systemtypes.WSKindProjectLogs:    {},
			systemtypes.WSKindContainerLogs:  {},
			systemtypes.WSKindContainerStats: {},
			systemtypes.WSKindContainerExec:  {},
			systemtypes.WSKindSystemStats:    {},
			systemtypes.WSKindServiceLogs:    {},
			systemtypes.WSKindDiagnostics:    {},
		},
		connections: make(map[string]systemtypes.WebSocketConnectionInfo),
	}
}

// Snapshot returns a point-in-time copy of the active connection counts.
func (m *WebSocketMetrics) Snapshot() systemtypes.WebSocketMetricsSnapshot {
	return systemtypes.WebSocketMetricsSnapshot{
		ProjectLogsActive:   m.counters[systemtypes.WSKindProjectLogs].Load(),
		ContainerLogsActive: m.counters[systemtypes.WSKindContainerLogs].Load(),
		ContainerStats:      m.counters[systemtypes.WSKindContainerStats].Load(),
		ContainerExec:       m.counters[systemtypes.WSKindContainerExec].Load(),
		SystemStats:         m.counters[systemtypes.WSKindSystemStats].Load(),
		ServiceLogsActive:   m.counters[systemtypes.WSKindServiceLogs].Load(),
		DiagnosticsActive:   m.counters[systemtypes.WSKindDiagnostics].Load(),
	}
}

// ObserveConnections reports active connection counts by kind through the arcane.websocket.connections gauge.
func (m *WebSocketMetrics) ObserveConnections(meter metric.Meter) error {
	gauge, err := meter.Int64ObservableGauge("arcane.websocket.connections",
		metric.WithDescription("Active WebSocket connections by kind"),
		metric.WithUnit("{connection}"),
	)
	if err != nil {
		return err
	}
	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		for kind, counter := range m.counters {
			o.ObserveInt64(gauge, counter.Load(), metric.WithAttributes(attribute.String("kind", kind)))
		}
		return nil
	}, gauge)
	return err
}

// Connections returns a snapshot of all tracked WebSocket connections.
func (m *WebSocketMetrics) Connections() []systemtypes.WebSocketConnectionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.AppendSeq(make([]systemtypes.WebSocketConnectionInfo, 0, len(m.connections)), maps.Values(m.connections))
}

// RegisterConnection adds a connection to the tracker and increments its kind counter.
// Returns the assigned connection ID.
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
	if counter, ok := m.counters[info.Kind]; ok {
		counter.Add(1)
	}
	return info.ID
}

// UnregisterConnection removes a connection from the tracker and decrements its kind counter.
func (m *WebSocketMetrics) UnregisterConnection(id string) {
	m.mu.Lock()
	info := m.connections[id]
	delete(m.connections, id)
	m.mu.Unlock()
	if counter, ok := m.counters[info.Kind]; ok {
		counter.Add(-1)
	}
}
