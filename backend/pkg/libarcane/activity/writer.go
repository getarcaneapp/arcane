package activity

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
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

	dropped      atomic.Uint64
	lastDropWarn atomic.Int64
}

const (
	writerAppendQueueSize = 128
	// writerAppendBatchSize caps how many queued lines are drained into one
	// AppendMessages transaction.
	writerAppendBatchSize = 32

	writerDropWarningInterval = 5 * time.Second
)

type writerAppendMessage struct {
	level   types.ActivityMessageLevel
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
		return kit.Ternary(writer == nil, io.Discard, writer)
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
	go out.drainMessages(ctx)
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
		if message, ok := w.processLine(line).Get(); ok {
			messages = append(messages, message)
		}
	}
	w.mu.Unlock()

	for _, message := range messages {
		w.enqueueMessage(message)
	}

	return len(p), nil
}

func (w *Writer) Flush() {
	if flusher, ok := w.writer.(http.Flusher); ok {
		flusher.Flush()
	}
	w.mu.Lock()
	residual := w.processLine(strings.TrimSpace(string(w.buffer)))
	w.buffer = nil
	w.mu.Unlock()
	if message, ok := residual.Get(); ok {
		select {
		case w.queueCh <- writerQueueItem{message: &message}:
		case <-done(w.ctx):
			return
		}
	}

	flushDone := make(chan struct{})
	select {
	case w.queueCh <- writerQueueItem{flush: flushDone}:
	case <-done(w.ctx):
		return
	}
	select {
	case <-flushDone:
	case <-done(w.ctx):
		return
	}
}

// processLine records stream lines as activity messages verbatim.
// Docker operation output arrives as {"log":"<raw CLI line>"} frames; terminal
// failures as {"error":"..."} frames. Anything else passes through untouched.
func (w *Writer) processLine(line string) mo.Option[writerAppendMessage] {
	if line == "" || w.activityService == nil || w.activityID == "" {
		return mo.None[writerAppendMessage]()
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return mo.Some(writerAppendMessage{
			level:   types.ActivityMessageLevelInfo,
			message: line,
			step:    w.defaultStep,
		})
	}

	if errorValue, ok := payload["error"]; ok && errorValue != nil {
		return mo.Some(writerAppendMessage{
			level:   types.ActivityMessageLevelError,
			message: kit.ToString(errorValue),
			payload: payload,
			step:    w.defaultStep,
		})
	}

	if logValue, ok := payload["log"]; ok {
		message := kit.ToString(logValue)
		if strings.TrimSpace(message) == "" {
			return mo.None[writerAppendMessage]()
		}
		return mo.Some(writerAppendMessage{
			level:   types.ActivityMessageLevelInfo,
			message: message,
			step:    w.defaultStep,
		})
	}

	return mo.Some(writerAppendMessage{
		level:   types.ActivityMessageLevelInfo,
		message: line,
		payload: payload,
		step:    w.defaultStep,
	})
}

func (w *Writer) enqueueMessage(message writerAppendMessage) {
	select {
	case w.queueCh <- writerQueueItem{message: &message}:
	case <-done(w.ctx):
	default:
		w.dropped.Add(1)
		now := time.Now().UnixNano()
		lastWarned := w.lastDropWarn.Load()
		if now-lastWarned >= writerDropWarningInterval.Nanoseconds() && w.lastDropWarn.CompareAndSwap(lastWarned, now) {
			slog.WarnContext(w.persistCtx, "activity output queue full; dropping lines", "activityId", w.activityID, "droppedCount", w.dropped.Swap(0))
		}
	}
}

func (w *Writer) drainMessages(ctx context.Context) {
	for {
		select {
		case item := <-w.queueCh:
			w.drainQueueBatch(item)
		case <-done(ctx):
			// Cancellation stops intake, but the lines already accepted into
			// the queue — typically the ones explaining the cancellation —
			// still get persisted before the goroutine exits.
			w.drainRemaining()
			return
		}
	}
}

func (w *Writer) drainRemaining() {
	for {
		select {
		case item := <-w.queueCh:
			w.drainQueueBatch(item)
		default:
			return
		}
	}
}

// drainQueueBatch drains whatever is already queued behind item (up
// to writerAppendBatchSize lines) into a single AppendMessages call, so bulk
// docker output costs one transaction per batch instead of one per line.
// Flush signals encountered while draining close only after the write, since
// the messages queued before them are only then persisted.
func (w *Writer) drainQueueBatch(item writerQueueItem) {
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

func done(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}
