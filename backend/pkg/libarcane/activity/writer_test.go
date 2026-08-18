package activity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/stretchr/testify/require"
)

type recordingAppender struct {
	mu         sync.Mutex
	messages   []AppendMessageRequest
	batchSizes []int
}

func (a *recordingAppender) AppendMessage(_ context.Context, _ string, req AppendMessageRequest) (*activitytypes.Message, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = append(a.messages, req)
	return &activitytypes.Message{}, nil
}

func (a *recordingAppender) AppendMessages(_ context.Context, _ string, reqs []AppendMessageRequest) ([]activitytypes.Message, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = append(a.messages, reqs...)
	a.batchSizes = append(a.batchSizes, len(reqs))
	return make([]activitytypes.Message, len(reqs)), nil
}

func (a *recordingAppender) recorded() ([]AppendMessageRequest, []int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]AppendMessageRequest(nil), a.messages...), append([]int(nil), a.batchSizes...)
}

type failingWriter struct{}

func (f failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func TestWriterContinuesActivityCaptureWhenResponseWriterFailsInternal(t *testing.T) {
	appender := &recordingAppender{}
	writer := NewWriter(context.Background(), appender, "activity-1", failingWriter{}, "Pulling image")

	n, err := writer.Write([]byte("Downloading layer\n"))
	require.NoError(t, err)
	require.Equal(t, len("Downloading layer\n"), n)

	FlushWriter(writer)
	require.Eventually(t, func() bool {
		messages, _ := appender.recorded()
		return len(messages) == 1
	}, time.Second, 10*time.Millisecond)
	messages, _ := appender.recorded()
	require.Equal(t, "Downloading layer", messages[0].Message)
	require.Equal(t, activitytypes.MessageLevelInfo, messages[0].Level)
}

func TestWriterRecordsLogAndErrorFramesVerbatimInternal(t *testing.T) {
	appender := &recordingAppender{}
	writer := NewWriter(context.Background(), appender, "activity-1", io.Discard, "Deploying project")

	_, err := writer.Write([]byte(`{"log":"Container web-1  Created"}` + "\n" + `{"error":"Error response from daemon: conflict"}` + "\n"))
	require.NoError(t, err)

	FlushWriter(writer)
	require.Eventually(t, func() bool {
		messages, _ := appender.recorded()
		return len(messages) == 2
	}, time.Second, 10*time.Millisecond)
	messages, _ := appender.recorded()
	require.Equal(t, "Container web-1  Created", messages[0].Message)
	require.Equal(t, activitytypes.MessageLevelInfo, messages[0].Level)
	require.Equal(t, "Error response from daemon: conflict", messages[1].Message)
	require.Equal(t, activitytypes.MessageLevelError, messages[1].Level)
}

// gatedAppender blocks its first AppendMessages call until release is
// closed, letting a test queue lines behind a known-stalled drain goroutine.
type gatedAppender struct {
	recordingAppender
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
}

func (g *gatedAppender) AppendMessages(ctx context.Context, activityID string, reqs []AppendMessageRequest) ([]activitytypes.Message, error) {
	g.startedOnce.Do(func() { close(g.started) })
	<-g.release
	return g.recordingAppender.AppendMessages(ctx, activityID, reqs)
}

// TestWriterBatchesQueuedLinesIntoOneAppendInternal verifies lines already
// queued when the drain goroutine wakes are persisted through a single
// AppendMessages call, in order, rather than one call per line.
func TestWriterBatchesQueuedLinesIntoOneAppendInternal(t *testing.T) {
	appender := &gatedAppender{started: make(chan struct{}), release: make(chan struct{})}
	writer := NewWriter(context.Background(), appender, "activity-1", io.Discard, "Pulling image")

	_, err := writer.Write([]byte("layer 0\n"))
	require.NoError(t, err)
	select {
	case <-appender.started:
	case <-time.After(time.Second):
		t.Fatal("first append never started")
	}

	// The drain goroutine is stalled inside the first append; everything
	// written now queues up behind it.
	for i := 1; i < 10; i++ {
		_, err := writer.Write(fmt.Appendf(nil, "layer %d\n", i))
		require.NoError(t, err)
	}
	close(appender.release)

	require.Eventually(t, func() bool {
		messages, _ := appender.recorded()
		return len(messages) == 10
	}, time.Second, 10*time.Millisecond)

	messages, batchSizes := appender.recorded()
	for i, message := range messages {
		require.Equal(t, fmt.Sprintf("layer %d", i), message.Message)
	}
	require.Equal(t, []int{1, 9}, batchSizes)
}

func TestWriterReturnsWrappedWriteErrorWithoutActivityInternal(t *testing.T) {
	writer := NewWriter(context.Background(), nil, "", failingWriter{}, "Pulling image")

	_, err := writer.Write([]byte("Downloading layer\n"))
	require.ErrorContains(t, err, "client disconnected")
}

var _ MessageAppender = (*recordingAppender)(nil)
var _ io.Writer = failingWriter{}

// TestWriterPersistsQueuedLinesAfterCancelInternal verifies that cancelling
// the work context does not abandon lines the writer already accepted:
// batches persist on a detached context and the drain goroutine empties the
// queue before exiting, since the lines queued at cancellation are usually
// the ones explaining it.
func TestWriterPersistsQueuedLinesAfterCancelInternal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	appender := &gatedAppender{started: make(chan struct{}), release: make(chan struct{})}
	writer := NewWriter(ctx, appender, "activity-1", io.Discard, "Pulling image")

	_, err := writer.Write([]byte("layer 0\n"))
	require.NoError(t, err)
	select {
	case <-appender.started:
	case <-time.After(time.Second):
		t.Fatal("first append never started")
	}

	// The drain goroutine is stalled inside the first append; these queue up
	// behind it and are still pending when the context is cancelled.
	for i := 1; i < 10; i++ {
		_, err := writer.Write(fmt.Appendf(nil, "layer %d\n", i))
		require.NoError(t, err)
	}
	cancel()
	close(appender.release)

	require.Eventually(t, func() bool {
		messages, _ := appender.recorded()
		return len(messages) == 10
	}, time.Second, 10*time.Millisecond)

	messages, _ := appender.recorded()
	for i, message := range messages {
		require.Equal(t, fmt.Sprintf("layer %d", i), message.Message)
	}
}
