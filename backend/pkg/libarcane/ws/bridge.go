package ws

import (
	"context"
	"encoding/json"
	"time"
)

type LogMessage struct {
	Seq         uint64 `json:"seq"`
	Level       string `json:"level,omitempty"`
	Message     string `json:"message"`
	Timestamp   string `json:"timestamp"` // RFC3339(9) string
	Service     string `json:"service,omitempty"`
	ContainerID string `json:"containerId,omitempty"`
}

// ForwardLines forwards plain text lines to the hub.
func ForwardLines(ctx context.Context, hub *Hub, lines <-chan string) {
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-lines:
			if !ok {
				return
			}
			hub.Broadcast([]byte(line))
		}
	}
}

// ForwardLogJSON sends each LogMessage as its own JSON object frame.
func ForwardLogJSON(ctx context.Context, hub *Hub, logs <-chan LogMessage) {
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-logs:
			if !ok {
				return
			}
			if m.Timestamp == "" {
				m.Timestamp = NowRFC3339()
			}
			if b, err := json.Marshal(m); err == nil {
				hub.Broadcast(b)
			}
		}
	}
}

// ForwardLogJSONBatched batches log messages into a JSON array frame to reduce frame count.
// Flushes when maxBatch reached or flushInterval elapsed.
func ForwardLogJSONBatched(ctx context.Context, hub *Hub, logs <-chan LogMessage, maxBatch int, flushInterval time.Duration) {
	if maxBatch <= 1 {
		ForwardLogJSON(ctx, hub, logs)
		return
	}
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	buf := make([]LogMessage, 0, maxBatch)

	flush := func() {
		if len(buf) == 0 {
			return
		}
		b, err := json.Marshal(buf)
		if err == nil {
			hub.Broadcast(b)
		}
		buf = buf[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case m, ok := <-logs:
			if !ok {
				flush()
				return
			}
			if m.Timestamp == "" {
				m.Timestamp = NowRFC3339()
			}
			buf = append(buf, m)
			if len(buf) >= maxBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
