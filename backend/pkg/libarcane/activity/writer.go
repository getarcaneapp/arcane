package activity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/backend/internal/models"
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
	layers map[string]layerProgress
}

const writerAppendQueueSize = 128

type layerProgress struct {
	current int64
	total   int64
	status  string
}

type writerAppendMessage struct {
	level    models.ActivityMessageLevel
	message  string
	payload  models.JSON
	progress *int
	step     string
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
		layers:          map[string]layerProgress{},
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
		if message, ok := w.processLineInternal(line); ok {
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

func (w *Writer) processLineInternal(line string) (writerAppendMessage, bool) {
	if line == "" || w.activityService == nil || w.activityID == "" {
		return writerAppendMessage{}, false
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return writerAppendMessage{
			level:   models.ActivityMessageLevelInfo,
			message: line,
			step:    w.defaultStep,
		}, true
	}

	message, level, step, progress := w.describePayloadInternal(payload)
	if message == "" {
		return writerAppendMessage{}, false
	}
	return writerAppendMessage{
		level:    level,
		message:  message,
		payload:  models.JSON(payload),
		progress: progress,
		step:     step,
	}, true
}

func (w *Writer) describePayloadInternal(payload map[string]any) (string, models.ActivityMessageLevel, string, *int) {
	level := models.ActivityMessageLevelInfo
	step := w.defaultStep

	if errorValue, ok := payload["error"]; ok && errorValue != nil {
		level = models.ActivityMessageLevelError
		return valueToStringInternal(errorValue), level, step, nil
	}

	if typ := strings.TrimSpace(valueToStringInternal(payload["type"])); typ != "" {
		if typ == "container" {
			return containerEventMessageInternal(payload), level, step, nil
		}
		if phase := strings.TrimSpace(valueToStringInternal(payload["phase"])); phase != "" {
			step = phaseStepInternal(typ, phase, step)
			progress := phaseProgressInternal(phase, payload["progressDetail"])
			return phaseMessageInternal(typ, phase, payload), level, step, progress
		}
	}

	if stream := strings.TrimSpace(valueToStringInternal(payload["stream"])); stream != "" {
		return stream, level, fallbackStepInternal(step, "Building image"), nil
	}

	status := strings.TrimSpace(valueToStringInternal(payload["status"]))
	id := strings.TrimSpace(valueToStringInternal(payload["id"]))
	progressText := strings.TrimSpace(valueToStringInternal(payload["progress"]))
	progress := w.updateLayerProgressInternal(id, status, payload["progressDetail"])

	if status != "" {
		parts := []string{status}
		if id != "" {
			parts = append(parts, id)
		}
		if progressText != "" {
			parts = append(parts, progressText)
		}
		return strings.Join(parts, " · "), level, statusStepInternal(status, step), progress
	}

	return "", level, step, nil
}

func (w *Writer) updateLayerProgressInternal(id, status string, rawDetail any) *int {
	if id == "" {
		return nil
	}

	layer := w.layers[id]
	layer.status = status
	if detail, ok := rawDetail.(map[string]any); ok {
		layer.current = numberToInt64Internal(detail["current"])
		layer.total = numberToInt64Internal(detail["total"])
	}
	w.layers[id] = layer

	if len(w.layers) == 0 {
		return nil
	}

	var weighted float64
	for _, item := range w.layers {
		statusLower := strings.ToLower(item.status)
		switch {
		case layerCompleteInternal(statusLower):
			weighted++
		case strings.Contains(statusLower, "extracting"):
			weighted += 0.95
		case strings.Contains(statusLower, "verifying"):
			weighted += 0.92
		case strings.Contains(statusLower, "download complete"):
			weighted += 0.85
		case item.total > 0:
			weighted += min((float64(item.current)/float64(item.total))*0.85, 0.85)
		case strings.Contains(statusLower, "downloading") || strings.Contains(statusLower, "pulling"):
			weighted += 0.05
		}
	}

	progress := max(min(int((weighted/float64(len(w.layers)))*100), 100), 0)
	return &progress
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
		Level:    message.level,
		Message:  message.message,
		Payload:  message.payload,
		Progress: message.progress,
		Step:     message.step,
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

func numberToInt64Internal(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case json.Number:
		out, _ := typed.Int64()
		return out
	default:
		return 0
	}
}

func layerCompleteInternal(status string) bool {
	return strings.Contains(status, "pull complete") ||
		strings.Contains(status, "already exists") ||
		strings.Contains(status, "downloaded newer image") ||
		strings.Contains(status, "image is up to date")
}

func statusStepInternal(status, fallback string) string {
	lower := strings.ToLower(status)
	switch {
	case strings.Contains(lower, "downloading") || strings.Contains(lower, "pulling"):
		return "Downloading layers"
	case strings.Contains(lower, "extracting"):
		return "Extracting layers"
	case strings.Contains(lower, "verifying") || strings.Contains(lower, "digest"):
		return "Verifying image"
	case strings.Contains(lower, "building"):
		return "Building image"
	}
	return fallback
}

func phaseStepInternal(typ, phase, fallback string) string {
	switch typ {
	case "deploy":
		switch phase {
		case "begin":
			return "Starting deployment"
		case "complete":
			return "Deployment complete"
		default:
			return "Deploying services"
		}
	case "build":
		switch phase {
		case "begin":
			return "Starting build"
		case "complete":
			return "Build complete"
		default:
			return "Building image"
		}
	}
	return fallback
}

func phaseMessageInternal(typ, phase string, payload map[string]any) string {
	if status := strings.TrimSpace(valueToStringInternal(payload["status"])); status != "" {
		return status
	}
	if service := strings.TrimSpace(valueToStringInternal(payload["service"])); service != "" {
		return fmt.Sprintf("%s %s: %s", typ, phase, service)
	}
	return fmt.Sprintf("%s %s", typ, phase)
}

func phaseProgressInternal(phase string, rawDetail any) *int {
	switch phase {
	case "begin":
		return new(5)
	case "complete":
		return new(100)
	default:
		return progressDetailPercentInternal(rawDetail)
	}
}

func progressDetailPercentInternal(rawDetail any) *int {
	detail, ok := rawDetail.(map[string]any)
	if !ok {
		return nil
	}

	current := numberToInt64Internal(detail["current"])
	total := numberToInt64Internal(detail["total"])

	var progress int64
	switch {
	case total > 0:
		progress = (current * 100) / total
	default:
		progress = current
	}

	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}

	return new(int(progress))
}

func containerEventMessageInternal(payload map[string]any) string {
	service := strings.TrimSpace(valueToStringInternal(payload["service"]))
	state := strings.TrimSpace(valueToStringInternal(payload["state"]))
	status := strings.TrimSpace(valueToStringInternal(payload["status"]))

	if service == "" {
		service = "unknown"
	}

	if status != "" {
		return fmt.Sprintf("Container %s: %s", service, status)
	}
	if state != "" {
		return fmt.Sprintf("Container %s: %s", service, state)
	}
	return "Container " + service
}

func fallbackStepInternal(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
