package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registerCollector registers a client on the hub and returns its send channel.
// Must be called before broadcasting so the client is present to receive messages.
func registerCollector(t *testing.T, h *Hub, bufSize int) *Client {
	t.Helper()
	_, sc, cleanup := newTestWSPair(t)
	t.Cleanup(cleanup)

	c := NewClient(sc, bufSize)
	h.register <- c

	require.Eventually(t, func() bool {
		return h.ClientCount() >= 1
	}, time.Second, 5*time.Millisecond)

	return c
}

// drainClient reads count messages from the client's send channel.
func drainClient(t *testing.T, c *Client, count int, timeout time.Duration) [][]byte {
	t.Helper()
	var msgs [][]byte
	deadline := time.After(timeout)
	for range count {
		select {
		case msg := <-c.send:
			msgs = append(msgs, msg)
		case <-deadline:
			t.Fatalf("timed out waiting for messages, got %d/%d", len(msgs), count)
		}
	}
	return msgs
}

func TestForwardLines(t *testing.T) {
	h := NewHub(100)
	ctx := t.Context()
	go h.Run(ctx)

	// Register collector BEFORE starting forwarder
	c := registerCollector(t, h, 256)

	lines := make(chan string, 5)
	lines <- "line 1"
	lines <- "line 2"
	lines <- "line 3"
	close(lines)

	go ForwardLines(ctx, h, lines)

	msgs := drainClient(t, c, 3, 2*time.Second)
	assert.Equal(t, "line 1", string(msgs[0]))
	assert.Equal(t, "line 2", string(msgs[1]))
	assert.Equal(t, "line 3", string(msgs[2]))
}

func TestForwardLines_ContextCancellation(t *testing.T) {
	h := NewHub(100)
	hubCtx := t.Context()
	go h.Run(hubCtx)

	lines := make(chan string)
	forwardCtx, forwardCancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		ForwardLines(forwardCtx, h, lines)
		close(done)
	}()

	forwardCancel()

	select {
	case <-done:
		// ForwardLines exited as expected
	case <-time.After(time.Second):
		t.Fatal("ForwardLines did not exit after context cancellation")
	}
}

func TestForwardLogJSON(t *testing.T) {
	h := NewHub(100)
	ctx := t.Context()
	go h.Run(ctx)

	c := registerCollector(t, h, 256)

	logs := make(chan LogMessage, 3)
	logs <- LogMessage{Seq: 1, Level: "stdout", Message: "hello", Timestamp: "2024-01-15T10:30:45Z"}
	logs <- LogMessage{Seq: 2, Level: "stderr", Message: "error", Timestamp: "2024-01-15T10:30:46Z"}
	close(logs)

	go ForwardLogJSON(ctx, h, logs)

	msgs := drainClient(t, c, 2, 2*time.Second)

	var m1, m2 LogMessage
	require.NoError(t, json.Unmarshal(msgs[0], &m1))
	require.NoError(t, json.Unmarshal(msgs[1], &m2))

	assert.Equal(t, uint64(1), m1.Seq)
	assert.Equal(t, "hello", m1.Message)
	assert.Equal(t, uint64(2), m2.Seq)
	assert.Equal(t, "error", m2.Message)
}

func TestForwardLogJSON_FillsEmptyTimestamp(t *testing.T) {
	h := NewHub(100)
	ctx := t.Context()
	go h.Run(ctx)

	c := registerCollector(t, h, 256)

	logs := make(chan LogMessage, 1)
	logs <- LogMessage{Seq: 1, Message: "no timestamp"}
	close(logs)

	go ForwardLogJSON(ctx, h, logs)

	msgs := drainClient(t, c, 1, 2*time.Second)

	var m LogMessage
	require.NoError(t, json.Unmarshal(msgs[0], &m))
	assert.NotEmpty(t, m.Timestamp, "timestamp should be auto-filled")

	_, err := time.Parse(time.RFC3339Nano, m.Timestamp)
	assert.NoError(t, err)
}

func TestForwardLogJSONBatched(t *testing.T) {
	h := NewHub(100)
	ctx := t.Context()
	go h.Run(ctx)

	c := registerCollector(t, h, 256)

	logs := make(chan LogMessage, 10)
	for i := range 5 {
		logs <- LogMessage{
			Seq:       uint64(i + 1), //nolint:gosec // range index is non-negative
			Message:   "msg",
			Timestamp: "2024-01-15T10:30:45Z",
		}
	}
	close(logs)

	go ForwardLogJSONBatched(ctx, h, logs, 5, 100*time.Millisecond)

	msgs := drainClient(t, c, 1, 2*time.Second)

	var batch []LogMessage
	require.NoError(t, json.Unmarshal(msgs[0], &batch))
	assert.Len(t, batch, 5, "all messages should be in a single batch")
}

func TestForwardLogJSONBatched_FlushesOnInterval(t *testing.T) {
	h := NewHub(100)
	ctx := t.Context()
	go h.Run(ctx)

	c := registerCollector(t, h, 256)

	logs := make(chan LogMessage, 10)
	logs <- LogMessage{Seq: 1, Message: "a", Timestamp: "2024-01-15T10:30:45Z"}
	logs <- LogMessage{Seq: 2, Message: "b", Timestamp: "2024-01-15T10:30:46Z"}

	go ForwardLogJSONBatched(ctx, h, logs, 10, 50*time.Millisecond)

	msgs := drainClient(t, c, 1, 2*time.Second)

	var batch []LogMessage
	require.NoError(t, json.Unmarshal(msgs[0], &batch))
	assert.Len(t, batch, 2)
}

func TestForwardLogJSONBatched_MultipleBatches(t *testing.T) {
	h := NewHub(100)
	ctx := t.Context()
	go h.Run(ctx)

	c := registerCollector(t, h, 256)

	logs := make(chan LogMessage, 10)
	for i := range 6 {
		logs <- LogMessage{
			Seq:       uint64(i + 1), //nolint:gosec // range index is non-negative
			Message:   "msg",
			Timestamp: "2024-01-15T10:30:45Z",
		}
	}
	close(logs)

	go ForwardLogJSONBatched(ctx, h, logs, 3, time.Second)

	msgs := drainClient(t, c, 2, 2*time.Second)

	var batch1, batch2 []LogMessage
	require.NoError(t, json.Unmarshal(msgs[0], &batch1))
	require.NoError(t, json.Unmarshal(msgs[1], &batch2))
	assert.Len(t, batch1, 3)
	assert.Len(t, batch2, 3)
}

func TestForwardLogJSONBatched_DelegatesToUnbatchedWhenMaxBatchOne(t *testing.T) {
	h := NewHub(100)
	ctx := t.Context()
	go h.Run(ctx)

	c := registerCollector(t, h, 256)

	logs := make(chan LogMessage, 3)
	logs <- LogMessage{Seq: 1, Message: "single", Timestamp: "2024-01-15T10:30:45Z"}
	logs <- LogMessage{Seq: 2, Message: "items", Timestamp: "2024-01-15T10:30:46Z"}
	close(logs)

	go ForwardLogJSONBatched(ctx, h, logs, 1, time.Second)

	msgs := drainClient(t, c, 2, 2*time.Second)

	// Each message should be a single object, not an array
	var m1 LogMessage
	require.NoError(t, json.Unmarshal(msgs[0], &m1))
	assert.Equal(t, "single", m1.Message)
}

func TestForwardLogJSONBatched_FlushesOnChannelClose(t *testing.T) {
	h := NewHub(100)
	ctx := t.Context()
	go h.Run(ctx)

	c := registerCollector(t, h, 256)

	logs := make(chan LogMessage, 10)
	// Send 2 messages but maxBatch is 100 — should flush on channel close
	logs <- LogMessage{Seq: 1, Message: "flush on close", Timestamp: "2024-01-15T10:30:45Z"}
	logs <- LogMessage{Seq: 2, Message: "also flushed", Timestamp: "2024-01-15T10:30:46Z"}
	close(logs)

	go ForwardLogJSONBatched(ctx, h, logs, 100, time.Hour)

	msgs := drainClient(t, c, 1, 2*time.Second)

	var batch []LogMessage
	require.NoError(t, json.Unmarshal(msgs[0], &batch))
	assert.Len(t, batch, 2)
	assert.Equal(t, "flush on close", batch[0].Message)
	assert.Equal(t, "also flushed", batch[1].Message)
}

func TestForwardLogJSONBatched_FlushesOnContextCancel(t *testing.T) {
	h := NewHub(100)
	hubCtx := t.Context()
	go h.Run(hubCtx)

	c := registerCollector(t, h, 256)

	// Use an unbuffered channel so we can control exactly when messages are sent
	logs := make(chan LogMessage)
	forwardCtx, forwardCancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		ForwardLogJSONBatched(forwardCtx, h, logs, 100, time.Hour)
		close(done)
	}()

	// Send one message
	logs <- LogMessage{Seq: 1, Message: "flush on cancel", Timestamp: "2024-01-15T10:30:45Z"}

	// Give the forwarder time to buffer it
	time.Sleep(50 * time.Millisecond)
	forwardCancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ForwardLogJSONBatched did not exit after context cancellation")
	}

	// The buffered message should have been flushed
	msgs := drainClient(t, c, 1, 2*time.Second)
	var batch []LogMessage
	require.NoError(t, json.Unmarshal(msgs[0], &batch))
	assert.Len(t, batch, 1)
	assert.Equal(t, "flush on cancel", batch[0].Message)
}
