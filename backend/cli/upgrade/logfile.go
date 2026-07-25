package upgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.getarcane.app/sys/atomic"
)

const logFilePerm = 0o644

// messageOnlyHandler renders a record as its message plus flattened attrs, with
// none of the level/time/source framing a text handler adds. The upgrade log is
// read by humans watching an upgrade, so it stays terse.
type messageOnlyHandler struct {
	mu       *sync.Mutex
	w        io.Writer
	minLevel slog.Level
	attrs    []slog.Attr
	groups   []string
}

// LogWriter accumulates the upgrade log in memory and rewrites the whole file
// through atomic.WriteFile on every entry. The upgrade recreates the container
// it is reporting on, so a run can be cut short at any point -- rewriting
// atomically per entry means the file on disk is always a complete, untorn log
// up to the last entry emitted, with no partial line ever visible to whoever
// reads it afterwards.
type LogWriter struct {
	mu   sync.Mutex
	path string
	buf  []byte
}

func (w *LogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)
	if err := atomic.WriteFile(w.path, w.buf, logFilePerm); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Name reports the path of the log file.
func (w *LogWriter) Name() string { return w.path }

// SetupMessageOnlyLogFile writes normal slog output to stdout and message-only
// output to a timestamped file under dataDir. Every entry is persisted as it is
// logged, so the returned writer needs no closing -- it is returned so the
// caller can report where the log landed.
func SetupMessageOnlyLogFile(dataDir string, filePrefix string, minLevel slog.Level) (*LogWriter, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("dataDir is required")
	}
	if strings.TrimSpace(filePrefix) == "" {
		filePrefix = "arcane-upgrade"
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	logFile := &LogWriter{path: filepath.Join(dataDir, fmt.Sprintf("%s-%d.log", filePrefix, time.Now().Unix()))}
	if err := atomic.WriteFile(logFile.path, nil, logFilePerm); err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}

	stdoutHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: minLevel, AddSource: true})
	fileHandler := newMessageOnlyHandler(logFile, minLevel)
	slog.SetDefault(slog.New(slog.NewMultiHandler(stdoutHandler, fileHandler)))
	return logFile, nil
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
			prev := h.groups
			if a.Key != "" {
				h.groups = append(h.groups, a.Key)
			}
			for _, grouped := range a.Value.Group() {
				appendAttr(grouped)
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
		return strconv.Quote(fmt.Sprint(v.Group()))
	case slog.KindLogValuer:
		return formatSlogValue(v.Resolve())
	default:
		return strconv.Quote(fmt.Sprint(v.Any()))
	}
}
