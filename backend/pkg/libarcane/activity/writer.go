package activity

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"

	"github.com/samber/mo"
)

type Writer struct {
	ctx context.Context
	// persistCtx is ctx detached from cancellation. Cancellation usually
	// races the last — most diagnostic — output lines (the error frames
	// explaining it), so batches persist on persistCtx while ctx only bounds
	// the drain goroutine's intake, mirroring CompleteActivity's detached
	// terminal write.
	persistCtx      context.Context
	activityService MessageAppender
	activityID      string
	writer          io.Writer
	defaultStep     string
	queueCh         chan writerQueueItem

	mu     sync.Mutex
	buffer []byte
}

const (
	writerAppendQueueSize = 128
	// writerAppendBatchSize caps how many queued lines are drained into one
	// AppendMessages transaction.
	writerAppendBatchSize = 32
)

type writerAppendMessage struct {
	level   activitytypes.MessageLevel
	message string
	payload database.JSON
	step    string
}

type writerQueueItem struct {
	message *writerAppendMessage
	flush   chan struct{}
}

func NewWriter(ctx context.Context, activityService MessageAppender, activityID string, writer io.Writer, defaultStep string) io.Writer {
	if activityService == nil || strings.TrimSpace(activityID) == "" {
		if writer == nil {
			return io.Discard
		}
		return writer
	}
	if existing, ok := writer.(*Writer); ok {
		return existing
	}
	persistCtx := ctx
	if persistCtx != nil {
		persistCtx = context.WithoutCancel(persistCtx)
	}
	out := &Writer{
		ctx:             ctx,
		persistCtx:      persistCtx,
		activityService: activityService,
		activityID:      strings.TrimSpace(activityID),
		writer:          writer,
		defaultStep:     strings.TrimSpace(defaultStep),
		queueCh:         make(chan writerQueueItem, writerAppendQueueSize),
	}
	go out.drainMessagesInternal(ctx)
	return out
}

func (w *Writer) Write(p []byte) (int, error) {
	if w.writer != nil {
		// Keep activity capture alive when the client-side response stream disconnects.
		_, _ = w.writer.Write(p)
	}

	w.mu.Lock()
	var messages []writerAppendMessage
	w.buffer = append(w.buffer, p...)
	for {
		idx := bytes.IndexByte(w.buffer, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimSpace(string(w.buffer[:idx]))
		w.buffer = w.buffer[idx+1:]
		if message, ok := w.processLineInternal(line).Get(); ok {
			messages = append(messages, message)
		}
	}
	w.mu.Unlock()

	for _, message := range messages {
		w.enqueueMessageInternal(message)
	}

	return len(p), nil
}

func (w *Writer) Flush() {
	if flusher, ok := w.writer.(http.Flusher); ok {
		flusher.Flush()
	}
	flushDone := make(chan struct{})
	select {
	case w.queueCh <- writerQueueItem{flush: flushDone}:
	case <-doneInternal(w.ctx):
		return
	default:
		return
	}
	select {
	case <-flushDone:
	case <-doneInternal(w.ctx):
		return
	}
}

// processLineInternal records stream lines as activity messages verbatim.
// Docker operation output arrives as {"log":"<raw CLI line>"} frames; terminal
// failures as {"error":"..."} frames. Anything else passes through untouched.
func (w *Writer) processLineInternal(line string) mo.Option[writerAppendMessage] {
	if line == "" || w.activityService == nil || w.activityID == "" {
		return mo.None[writerAppendMessage]()
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return mo.Some(writerAppendMessage{
			level:   activitytypes.MessageLevelInfo,
			message: line,
			step:    w.defaultStep,
		})
	}

	if errorValue, ok := payload["error"]; ok && errorValue != nil {
		return mo.Some(writerAppendMessage{
			level:   activitytypes.MessageLevelError,
			message: utils.ToString(errorValue),
			payload: payload,
			step:    w.defaultStep,
		})
	}

	if logValue, ok := payload["log"]; ok {
		message := utils.ToString(logValue)
		if strings.TrimSpace(message) == "" {
			return mo.None[writerAppendMessage]()
		}
		return mo.Some(writerAppendMessage{
			level:   activitytypes.MessageLevelInfo,
			message: message,
			step:    w.defaultStep,
		})
	}

	return mo.Some(writerAppendMessage{
		level:   activitytypes.MessageLevelInfo,
		message: line,
		payload: payload,
		step:    w.defaultStep,
	})
}

func (w *Writer) enqueueMessageInternal(message writerAppendMessage) {
	select {
	case w.queueCh <- writerQueueItem{message: &message}:
	case <-doneInternal(w.ctx):
		return
	default:
		return
	}
}

func (w *Writer) drainMessagesInternal(ctx context.Context) {
	for {
		select {
		case item := <-w.queueCh:
			w.drainQueueBatchInternal(item)
		case <-doneInternal(ctx):
			// Cancellation stops intake, but the lines already accepted into
			// the queue — typically the ones explaining the cancellation —
			// still get persisted before the goroutine exits.
			w.drainRemainingInternal()
			return
		}
	}
}

func (w *Writer) drainRemainingInternal() {
	for {
		select {
		case item := <-w.queueCh:
			w.drainQueueBatchInternal(item)
		default:
			return
		}
	}
}

// drainQueueBatchInternal drains whatever is already queued behind item (up
// to writerAppendBatchSize lines) into a single AppendMessages call, so bulk
// docker output costs one transaction per batch instead of one per line.
// Flush signals encountered while draining close only after the write, since
// the messages queued before them are only then persisted.
func (w *Writer) drainQueueBatchInternal(item writerQueueItem) {
	var batch []AppendMessageRequest
	var flushes []chan struct{}

	collect := func(queued writerQueueItem) {
		if queued.flush != nil {
			flushes = append(flushes, queued.flush)
			return
		}
		if queued.message == nil {
			return
		}
		batch = append(batch, AppendMessageRequest{
			Level:   queued.message.level,
			Message: queued.message.message,
			Payload: queued.message.payload,
			Step:    queued.message.step,
		})
	}

	collect(item)
drain:
	for len(batch) < writerAppendBatchSize {
		select {
		case queued := <-w.queueCh:
			collect(queued)
		default:
			break drain
		}
	}

	if len(batch) > 0 && w.persistCtx != nil {
		if _, err := w.activityService.AppendMessages(w.persistCtx, w.activityID, batch); err != nil {
			slog.WarnContext(w.persistCtx, "failed to persist activity output batch", "activityId", w.activityID, "lines", len(batch), "error", err)
		}
	}
	// Flushes release even when the append failed: Flush carries no error
	// path, and holding them would stall activity completion on the very
	// cancellation that typically caused the failure.
	for _, flushDone := range flushes {
		close(flushDone)
	}
}

func doneInternal(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}
