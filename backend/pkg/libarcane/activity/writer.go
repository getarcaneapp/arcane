package activity

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/samber/mo"
)

type Writer struct {
	ctx             context.Context
	activityService MessageAppender
	activityID      string
	writer          io.Writer
	defaultStep     string
	queueCh         chan writerQueueItem

	mu     sync.Mutex
	buffer []byte
}

const writerAppendQueueSize = 128

type writerAppendMessage struct {
	level   models.ActivityMessageLevel
	message string
	payload models.JSON
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
	out := &Writer{
		ctx:             ctx,
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
	messages := []writerAppendMessage{}
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
			level:   models.ActivityMessageLevelInfo,
			message: line,
			step:    w.defaultStep,
		})
	}

	if errorValue, ok := payload["error"]; ok && errorValue != nil {
		return mo.Some(writerAppendMessage{
			level:   models.ActivityMessageLevelError,
			message: valueToStringInternal(errorValue),
			payload: models.JSON(payload),
			step:    w.defaultStep,
		})
	}

	if logValue, ok := payload["log"]; ok {
		message := valueToStringInternal(logValue)
		if strings.TrimSpace(message) == "" {
			return mo.None[writerAppendMessage]()
		}
		return mo.Some(writerAppendMessage{
			level:   models.ActivityMessageLevelInfo,
			message: message,
			step:    w.defaultStep,
		})
	}

	return mo.Some(writerAppendMessage{
		level:   models.ActivityMessageLevelInfo,
		message: line,
		payload: models.JSON(payload),
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
			if item.flush != nil {
				close(item.flush)
				continue
			}
			if item.message != nil {
				w.appendMessageInternal(ctx, *item.message)
			}
		case <-doneInternal(ctx):
			return
		}
	}
}

func doneInternal(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}

func (w *Writer) appendMessageInternal(ctx context.Context, message writerAppendMessage) {
	if ctx == nil {
		return
	}
	if _, err := w.activityService.AppendMessage(ctx, w.activityID, AppendMessageRequest{
		Level:   message.level,
		Message: message.message,
		Payload: message.payload,
		Step:    message.step,
	}); err != nil {
		return
	}
}

func valueToStringInternal(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}
