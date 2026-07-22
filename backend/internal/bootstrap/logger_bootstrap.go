package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/lmittmann/tint"
	slogGorm "github.com/orandin/slog-gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"gorm.io/gorm/logger"
)

type requestLogHandler struct {
	handler slog.Handler
}

type attrFilterHandler struct {
	handler  slog.Handler
	dropKeys map[string]struct{}
}

type stackAttrHandler struct {
	handler slog.Handler
	attrs   []slog.Attr
}

func (h *stackAttrHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *stackAttrHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < slog.LevelError {
		return h.handler.Handle(ctx, r)
	}

	attrs := append([]slog.Attr(nil), h.attrs...)
	r.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})

	var stackTracer interface{ StackTrace() errors.StackTrace }
	for _, attr := range attrs {
		attr.Value = attr.Value.Resolve()
		if attr.Value.Kind() != slog.KindAny {
			continue
		}
		err, ok := attr.Value.Any().(error)
		if ok && errors.As(err, &stackTracer) {
			r.AddAttrs(slog.String("stack", fmt.Sprintf("%+v", stackTracer.StackTrace())))
			break
		}
	}

	return h.handler.Handle(ctx, r)
}

func (h *stackAttrHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	boundAttrs := append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &stackAttrHandler{handler: h.handler.WithAttrs(attrs), attrs: boundAttrs}
}

func (h *stackAttrHandler) WithGroup(name string) slog.Handler {
	return &stackAttrHandler{handler: h.handler.WithGroup(name), attrs: h.attrs}
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
		condensedAttrs = append(condensedAttrs, normalizeRequestLogErrorInternal(a))
	}

	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	newRecord.AddAttrs(condensedAttrs...)

	return h.handler.Handle(ctx, newRecord)
}

func normalizeRequestLogErrorInternal(a slog.Attr) slog.Attr {
	if a.Key != "error" {
		return a
	}
	if value, ok := a.Value.Any().(map[string]any); ok {
		return slog.String("error", fmt.Sprint(value["message"]))
	}
	return a
}

func replaceErrorAttrInternal(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindAny {
		if err, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, err.Error())
		}
	}
	return a
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

func SetupSlogLogger(cfg *config.Config) {
	var lvl slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	lv := new(slog.LevelVar)
	lv.Set(lvl)

	var h slog.Handler
	if cfg.LogJson {
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:       lv,
			ReplaceAttr: replaceErrorAttrInternal,
		})
	} else {
		h = tint.NewTextHandler(os.Stdout, &tint.Options{
			Level:       lv,
			TimeFormat:  "Jan 02 15:04:05.000",
			ReplaceAttr: replaceErrorAttrInternal,
		})
	}

	h = &requestLogHandler{handler: h}
	h = &stackAttrHandler{handler: h}

	slog.SetDefault(slog.New(h))
}

func BuildGormLogger(cfg *config.Config) logger.Interface {
	lvl := strings.ToLower(cfg.LogLevel)

	filteredHandler := &attrFilterHandler{
		handler:  slog.Default().Handler(),
		dropKeys: map[string]struct{}{slogGorm.SourceField: {}},
	}

	opts := []slogGorm.Option{
		slogGorm.WithHandler(filteredHandler),
		slogGorm.WithSlowThreshold(200 * time.Millisecond),
	}

	var defaultTypeLevel slog.Level
	switch lvl {
	case "debug":
		defaultTypeLevel = slog.LevelDebug
		// Trace all SQL messages only in debug
		opts = append(opts, slogGorm.WithTraceAll())
	case "warn", "warning":
		defaultTypeLevel = slog.LevelWarn
	case "error":
		defaultTypeLevel = slog.LevelError
	default:
		defaultTypeLevel = slog.LevelInfo
	}

	opts = append(opts,
		slogGorm.SetLogLevel(slogGorm.DefaultLogType, defaultTypeLevel),
		slogGorm.SetLogLevel(slogGorm.ErrorLogType, slog.LevelError),
		slogGorm.SetLogLevel(slogGorm.SlowQueryLogType, slog.LevelWarn),
	)

	return slogGorm.New(opts...)
}

func ConfigureGormLogger(cfg *config.Config) {
	database.SetGormLogger(BuildGormLogger(cfg))
}
