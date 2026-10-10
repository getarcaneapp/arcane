package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
	slogGorm "github.com/orandin/slog-gorm"
	"go.getarcane.app/streams/logs"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/getarcaneapp/arcane/backend/v2/api/ws"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
)

// logLevels maps LOG_LEVEL values to slog levels; unknown values map to the zero value, slog.LevelInfo.
var logLevels = map[string]slog.Level{
	"debug":   slog.LevelDebug,
	"warn":    slog.LevelWarn,
	"warning": slog.LevelWarn,
	"error":   slog.LevelError,
}

type requestLogHandler struct {
	handler slog.Handler
}

type attrFilterHandler struct {
	handler  slog.Handler
	dropKeys map[string]struct{}
}

// gormLogger omits SQL parameters and drops traces for canceled requests.
type gormLogger struct {
	logger.Interface
}

var (
	_ logger.Interface  = gormLogger{}
	_ gorm.ParamsFilter = gormLogger{}
)

func (l gormLogger) LogMode(level logger.LogLevel) logger.Interface {
	l.Interface = l.Interface.LogMode(level)
	return l
}

func (l gormLogger) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}

func (l gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	l.Interface.Trace(ctx, begin, fc, err)
}

func (h *requestLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *requestLogHandler) Handle(ctx context.Context, r slog.Record) error {
	var attrs []slog.Attr
	var requestAttrs, responseAttrs []slog.Attr
	var hasRequest, hasResponse bool

	r.Attrs(func(a slog.Attr) bool {
		a.Value = a.Value.Resolve()
		switch {
		case a.Key == "request" && a.Value.Kind() == slog.KindGroup:
			requestAttrs = a.Value.Group()
			hasRequest = true
		case a.Key == "response" && a.Value.Kind() == slog.KindGroup:
			responseAttrs = a.Value.Group()
			hasResponse = true
		default:
			attrs = append(attrs, a)
		}
		return true
	})

	if !hasRequest || !hasResponse {
		return h.handler.Handle(ctx, r)
	}

	condensedAttrs := make([]slog.Attr, 0, 10+len(attrs))
	debugAttrs := make([]slog.Attr, 0, 6)
	debug := h.handler.Enabled(ctx, slog.LevelDebug)

	for _, a := range requestAttrs {
		a.Value = a.Value.Resolve()
		switch a.Key {
		case "method", "path":
			condensedAttrs = append(condensedAttrs, a)
		case "host", "route":
			if debug {
				debugAttrs = append(debugAttrs, a)
			}
		case "length":
			if debug {
				a.Key = "request_length"
				debugAttrs = append(debugAttrs, a)
			}
		case "query", "referer":
			if debug && a.Value.Kind() == slog.KindString && a.Value.String() != "" {
				debugAttrs = append(debugAttrs, a)
			}
		}
	}

	for _, a := range responseAttrs {
		a.Value = a.Value.Resolve()
		switch a.Key {
		case "status":
			condensedAttrs = append(condensedAttrs, a)
		case "latency":
			a.Key = "duration"
			condensedAttrs = append(condensedAttrs, a)
		case "length":
			if debug {
				a.Key = "response_length"
				debugAttrs = append(debugAttrs, a)
			}
		}
	}

	condensedAttrs = append(condensedAttrs, debugAttrs...)
	for _, a := range attrs {
		if value, ok := a.Value.Any().(map[string]any); ok && a.Key == "error" {
			a = slog.String("error", fmt.Sprint(value["message"]))
		}
		condensedAttrs = append(condensedAttrs, a)
	}

	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	newRecord.AddAttrs(condensedAttrs...)

	return h.handler.Handle(ctx, newRecord)
}

func (h *requestLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &requestLogHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h *requestLogHandler) WithGroup(name string) slog.Handler {
	return &requestLogHandler{handler: h.handler.WithGroup(name)}
}

func (h *attrFilterHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *attrFilterHandler) Handle(ctx context.Context, r slog.Record) error {
	var filteredAttrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		if _, drop := h.dropKeys[a.Key]; drop {
			return true
		}
		filteredAttrs = append(filteredAttrs, a)
		return true
	})

	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	newRecord.AddAttrs(filteredAttrs...)

	return h.handler.Handle(ctx, newRecord)
}

func (h *attrFilterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &attrFilterHandler{handler: h.handler.WithAttrs(attrs), dropKeys: h.dropKeys}
}

func (h *attrFilterHandler) WithGroup(name string) slog.Handler {
	return &attrFilterHandler{handler: h.handler.WithGroup(name), dropKeys: h.dropKeys}
}

// telemetryLogHandler applies LOG_LEVEL to the OpenTelemetry bridge and strips raw query strings,
// URL queries, and webhook trigger tokens before records are exported.
type telemetryLogHandler struct {
	handler slog.Handler
	level   slog.Leveler
}

func (h *telemetryLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level.Level() && h.handler.Enabled(ctx, level)
}

func (h *telemetryLogHandler) Handle(ctx context.Context, r slog.Record) error {
	redacted := slog.NewRecord(r.Time, r.Level, redactSecrets(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		redacted.AddAttrs(redactLogAttrs([]slog.Attr{a})...)
		return true
	})
	return h.handler.Handle(ctx, redacted)
}

func (h *telemetryLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &telemetryLogHandler{handler: h.handler.WithAttrs(redactLogAttrs(attrs)), level: h.level}
}

func (h *telemetryLogHandler) WithGroup(name string) slog.Handler {
	return &telemetryLogHandler{handler: h.handler.WithGroup(name), level: h.level}
}

func redactLogAttrs(attrs []slog.Attr) []slog.Attr {
	redacted := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		a.Value = a.Value.Resolve()
		if err, ok := a.Value.Any().(error); ok && a.Value.Kind() == slog.KindAny {
			a.Value = slog.StringValue(err.Error())
		}
		switch {
		case a.Key == "query":
			continue
		case a.Value.Kind() == slog.KindString:
			a.Value = slog.StringValue(redactSecrets(a.Value.String()))
		case a.Value.Kind() == slog.KindGroup:
			a.Value = slog.GroupValue(redactLogAttrs(a.Value.Group())...)
		}
		redacted = append(redacted, a)
	}
	return redacted
}

// SetupSlogLogger installs the console logger and the diagnostics log tee. When
// telemetryHandler is set, records are also sent to it at the same level.
func SetupSlogLogger(cfg *config.Config, telemetryHandler slog.Handler) {
	lv := new(slog.LevelVar)
	lv.Set(logLevels[strings.ToLower(cfg.LogLevel)])

	replaceErrorAttr := func(_ []string, a slog.Attr) slog.Attr {
		if err, ok := a.Value.Any().(error); ok && a.Value.Kind() == slog.KindAny {
			return slog.String(a.Key, err.Error())
		}
		return a
	}

	var h slog.Handler
	if cfg.LogJson {
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:       lv,
			ReplaceAttr: replaceErrorAttr,
		})
	} else {
		h = tint.NewTextHandler(os.Stdout, &tint.Options{
			Level:       lv,
			TimeFormat:  "Jan 02 15:04:05.000",
			ReplaceAttr: replaceErrorAttr,
		})
	}

	if telemetryHandler != nil {
		h = slog.NewMultiHandler(h, &telemetryLogHandler{handler: telemetryHandler, level: lv})
	}

	h = &requestLogHandler{handler: h}

	slog.SetDefault(slog.New(logs.NewSlogHandler(h, ws.LogBroadcaster())))
}

func BuildGormLogger(cfg *config.Config) logger.Interface {
	lvl := logLevels[strings.ToLower(cfg.LogLevel)]

	filteredHandler := &attrFilterHandler{
		handler:  slog.Default().Handler(),
		dropKeys: map[string]struct{}{slogGorm.SourceField: {}},
	}

	opts := []slogGorm.Option{
		slogGorm.WithHandler(filteredHandler),
		slogGorm.WithSlowThreshold(200 * time.Millisecond),
		slogGorm.SetLogLevel(slogGorm.DefaultLogType, lvl),
		slogGorm.SetLogLevel(slogGorm.ErrorLogType, slog.LevelError),
		slogGorm.SetLogLevel(slogGorm.SlowQueryLogType, slog.LevelWarn),
	}
	// Trace all SQL messages only in debug.
	if lvl == slog.LevelDebug {
		opts = append(opts, slogGorm.WithTraceAll())
	}

	return gormLogger{Interface: slogGorm.New(opts...)}
}
