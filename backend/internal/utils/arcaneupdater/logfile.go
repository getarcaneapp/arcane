package arcaneupdater

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type teeHandler struct {
	a slog.Handler
	b slog.Handler
}

func (t teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return t.a.Enabled(ctx, level) || t.b.Enabled(ctx, level)
}

func (t teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var err error
	if t.a.Enabled(ctx, r.Level) {
		err = t.a.Handle(ctx, r)
	}
	if t.b.Enabled(ctx, r.Level) {
		if err2 := t.b.Handle(ctx, r); err == nil {
			err = err2
		}
	}
	return err
}

func (t teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return teeHandler{a: t.a.WithAttrs(attrs), b: t.b.WithAttrs(attrs)}
}

func (t teeHandler) WithGroup(name string) slog.Handler {
	return teeHandler{a: t.a.WithGroup(name), b: t.b.WithGroup(name)}
}

type messageOnlyHandler struct {
	mu       *sync.Mutex
	w        io.Writer
	minLevel slog.Level
	attrs    []slog.Attr
	groups   []string
}

func newMessageOnlyHandler(w io.Writer, minLevel slog.Level) *messageOnlyHandler {
	return &messageOnlyHandler{mu: &sync.Mutex{}, w: w, minLevel: minLevel}
}

func (h *messageOnlyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h *messageOnlyHandler) Handle(_ context.Context, r slog.Record) error {
	if h.mu != nil {
		h.mu.Lock()
		defer h.mu.Unlock()
	}

	line := r.Message
	appendAttr := func(a slog.Attr) {
		if a.Equal(slog.Attr{}) {
			return
		}
		key := a.Key
		if len(h.groups) > 0 {
			key = strings.Join(h.groups, ".") + "." + key
		}
		line += " " + key + "=" + formatSlogValue(a.Value)
	}

	for _, a := range h.attrs {
		appendAttr(a)
	}

	r.Attrs(func(a slog.Attr) bool {
		if a.Value.Kind() == slog.KindGroup {
			g := a.Value.Group()
			prev := h.groups
			if a.Key != "" {
				h.groups = append(h.groups, a.Key)
			}
			for _, ga := range g {
				appendAttr(ga)
			}
			h.groups = prev
			return true
		}
		appendAttr(a)
		return true
	})

	_, err := fmt.Fprintln(h.w, strings.TrimSpace(line))
	return err
}

func (h *messageOnlyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &messageOnlyHandler{
		mu:       h.mu,
		w:        h.w,
		minLevel: h.minLevel,
		attrs:    append(append([]slog.Attr{}, h.attrs...), attrs...),
		groups:   append([]string{}, h.groups...),
	}
}

func (h *messageOnlyHandler) WithGroup(name string) slog.Handler {
	groups := append([]string{}, h.groups...)
	if strings.TrimSpace(name) != "" {
		groups = append(groups, name)
	}
	return &messageOnlyHandler{
		mu:       h.mu,
		w:        h.w,
		minLevel: h.minLevel,
		attrs:    append([]slog.Attr{}, h.attrs...),
		groups:   groups,
	}
}

func formatSlogValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindAny:
		return strconv.Quote(fmt.Sprint(v.Any()))
	case slog.KindString:
		return strconv.Quote(v.String())
	case slog.KindInt64:
		return strconv.FormatInt(v.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(v.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(v.Float64(), 'f', -1, 64)
	case slog.KindTime:
		return strconv.Quote(v.Time().Format(time.RFC3339Nano))
	case slog.KindDuration:
		return strconv.Quote(v.Duration().String())
	case slog.KindBool:
		if v.Bool() {
			return "true"
		}
		return "false"
	case slog.KindGroup:
		// Groups are typically flattened earlier in Handle(), but handle defensively.
		return strconv.Quote(fmt.Sprint(v.Group()))
	case slog.KindLogValuer:
		return formatSlogValue(v.Resolve())
	default:
		return strconv.Quote(fmt.Sprint(v.Any()))
	}
}

// SetupMessageOnlyLogFile configures slog to write structured logs to stdout and a
// message-only format to a file under dataDir.
//
// The file format is: <msg> key=value key=value ... (no time/level/source) to keep
// upgrade logs concise for end users.
func SetupMessageOnlyLogFile(dataDir string, filePrefix string, minLevel slog.Level) (*os.File, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("dataDir is required")
	}
	if strings.TrimSpace(filePrefix) == "" {
		filePrefix = "arcane-upgrade"
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	logFileName := fmt.Sprintf("%s-%d.log", filePrefix, time.Now().Unix())
	logFilePath := filepath.Join(dataDir, logFileName)

	logFile, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	stdoutHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     minLevel,
		AddSource: true,
	})
	fileHandler := newMessageOnlyHandler(logFile, minLevel)

	slog.SetDefault(slog.New(teeHandler{a: stdoutHandler, b: fileHandler}))

	return logFile, nil
}
