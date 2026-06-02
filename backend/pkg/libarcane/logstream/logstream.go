// Package logstream captures the application's own slog output into a bounded
// in-memory ring buffer and fans it out to live subscribers. It backs the
// diagnostics endpoints' "recent logs" backlog and the live backend-log
// WebSocket stream. It deliberately depends only on the shared types package so
// both bootstrap (which installs the slog handler) and the API handlers (which
// read it) can use it without an import cycle.
package logstream

import (
	"context"
	"log/slog"
	"sync"

	"github.com/getarcaneapp/arcane/types/system"
)

// defaultRingCapacity is the number of recent log entries retained in memory.
const defaultRingCapacity = 1000

// subscriberBuffer is the per-subscriber channel buffer. Slow consumers have
// entries dropped rather than blocking the logger.
const subscriberBuffer = 256

// Entry is a single captured log record.
type Entry = system.LogEntry

// Broadcaster keeps a bounded ring buffer of recent log entries and fans new
// entries out to any active subscribers. It is safe for concurrent use.
type Broadcaster struct {
	mu    sync.Mutex
	buf   []Entry
	start int
	size  int
	capN  int
	subs  map[chan Entry]struct{}
}

// New returns a Broadcaster retaining up to capacity recent entries.
func New(capacity int) *Broadcaster {
	if capacity <= 0 {
		capacity = defaultRingCapacity
	}
	return &Broadcaster{
		buf:  make([]Entry, capacity),
		capN: capacity,
		subs: make(map[chan Entry]struct{}),
	}
}

// Append records an entry in the ring buffer and delivers it to subscribers.
// Delivery is non-blocking; a subscriber whose buffer is full drops the entry.
func (b *Broadcaster) Append(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size < b.capN {
		b.buf[(b.start+b.size)%b.capN] = e
		b.size++
	} else {
		b.buf[b.start] = e
		b.start = (b.start + 1) % b.capN
	}

	// Non-blocking sends under the lock: cancel() also takes the lock before
	// closing a subscriber channel, so we never send on a closed channel.
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Recent returns the buffered entries in chronological order (oldest first).
func (b *Broadcaster) Recent() []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]Entry, b.size)
	for i := range b.size {
		out[i] = b.buf[(b.start+i)%b.capN]
	}
	return out
}

// Subscribe registers a new live subscriber. It returns a receive-only channel
// of subsequent entries and a cancel func that unsubscribes and closes the
// channel. cancel is idempotent.
func (b *Broadcaster) Subscribe() (<-chan Entry, func()) {
	ch := make(chan Entry, subscriberBuffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, ch)
			close(ch)
			b.mu.Unlock()
		})
	}
	return ch, cancel
}

var defaultBroadcaster = New(defaultRingCapacity)

// Default returns the package-level Broadcaster singleton.
func Default() *Broadcaster { return defaultBroadcaster }

// slogHandler wraps a base slog.Handler, forwarding every record to it while
// also appending it to a Broadcaster.
type slogHandler struct {
	base slog.Handler
	b    *Broadcaster
}

// NewSlogHandler returns a slog.Handler that tees records to base and to b.
func NewSlogHandler(base slog.Handler, b *Broadcaster) slog.Handler {
	return &slogHandler{base: base, b: b}
}

func (h *slogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

func (h *slogHandler) Handle(ctx context.Context, r slog.Record) error {
	var attrs map[string]any
	if r.NumAttrs() > 0 {
		attrs = make(map[string]any, r.NumAttrs())
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = normalizeAttrValueInternal(a.Value)
			return true
		})
	}
	h.b.Append(Entry{
		Time:    r.Time,
		Level:   r.Level.String(),
		Message: r.Message,
		Attrs:   attrs,
	})
	return h.base.Handle(ctx, r)
}

func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &slogHandler{base: h.base.WithAttrs(attrs), b: h.b}
}

func (h *slogHandler) WithGroup(name string) slog.Handler {
	return &slogHandler{base: h.base.WithGroup(name), b: h.b}
}

func normalizeAttrValueInternal(value slog.Value) any {
	value = value.Resolve()

	switch value.Kind() {
	case slog.KindAny:
		return normalizeAnyValueInternal(value.Any())
	case slog.KindBool:
		return value.Bool()
	case slog.KindDuration:
		return value.Duration()
	case slog.KindFloat64:
		return value.Float64()
	case slog.KindGroup:
		return normalizeGroupAttrsInternal(value.Group())
	case slog.KindInt64:
		return value.Int64()
	case slog.KindLogValuer:
		return value.String()
	case slog.KindString:
		return value.String()
	case slog.KindTime:
		return value.Time()
	case slog.KindUint64:
		return value.Uint64()
	default:
		return value.String()
	}
}

func normalizeAnyValueInternal(value any) any {
	switch typed := value.(type) {
	case slog.Attr:
		return map[string]any{typed.Key: normalizeAttrValueInternal(typed.Value)}
	case []slog.Attr:
		return normalizeGroupAttrsInternal(typed)
	case slog.Value:
		return normalizeAttrValueInternal(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = normalizeAnyValueInternal(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = normalizeAnyValueInternal(item)
		}
		return out
	default:
		return value
	}
}

func normalizeGroupAttrsInternal(attrs []slog.Attr) map[string]any {
	out := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		if attr.Key == "" {
			continue
		}
		out[attr.Key] = normalizeAttrValueInternal(attr.Value)
	}
	return out
}
