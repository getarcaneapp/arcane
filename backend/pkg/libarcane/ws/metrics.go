package ws

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// WebSocketMetrics tracks active WebSocket connections and their counts.
type WebSocketMetrics struct {
	counters    map[string]*atomic.Int64
	seq         atomic.Uint64
	mu          sync.RWMutex
	connections map[string]types.WebSocketConnectionInfo
}

// NewWebSocketMetrics creates a new WebSocketMetrics instance.
func NewWebSocketMetrics() *WebSocketMetrics {
	return &WebSocketMetrics{
		counters: map[string]*atomic.Int64{
			types.WSKindProjectLogs:    {},
			types.WSKindContainerLogs:  {},
			types.WSKindContainerStats: {},
			types.WSKindContainerExec:  {},
			types.WSKindSystemStats:    {},
			types.WSKindServiceLogs:    {},
			types.WSKindDiagnostics:    {},
		},
		connections: make(map[string]types.WebSocketConnectionInfo),
	}
}

// Snapshot returns a point-in-time copy of the active connection counts.
func (m *WebSocketMetrics) Snapshot() types.WebSocketMetricsSnapshot {
	return types.WebSocketMetricsSnapshot{
		ProjectLogsActive:   m.counters[types.WSKindProjectLogs].Load(),
		ContainerLogsActive: m.counters[types.WSKindContainerLogs].Load(),
		ContainerStats:      m.counters[types.WSKindContainerStats].Load(),
		ContainerExec:       m.counters[types.WSKindContainerExec].Load(),
		SystemStats:         m.counters[types.WSKindSystemStats].Load(),
		ServiceLogsActive:   m.counters[types.WSKindServiceLogs].Load(),
		DiagnosticsActive:   m.counters[types.WSKindDiagnostics].Load(),
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
func (m *WebSocketMetrics) Connections() []types.WebSocketConnectionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.AppendSeq(make([]types.WebSocketConnectionInfo, 0, len(m.connections)), maps.Values(m.connections))
}

// RegisterConnection adds a connection to the tracker and increments its kind counter.
// Returns the assigned connection ID.
func (m *WebSocketMetrics) RegisterConnection(info types.WebSocketConnectionInfo) string {
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
