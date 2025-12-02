package sloggin

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

const (
	customAttributesCtxKey = "slog-gin.custom-attributes"
)

var (
	TraceIDKey   = "trace_id"
	SpanIDKey    = "span_id"
	RequestIDKey = "id"

	RequestBodyMaxSize  = 64 * 1024 // 64KB
	ResponseBodyMaxSize = 64 * 1024 // 64KB

	HiddenRequestHeaders = map[string]struct{}{
		"authorization": {},
		"cookie":        {},
		"set-cookie":    {},
		"x-auth-token":  {},
		"x-csrf-token":  {},
		"x-xsrf-token":  {},
	}
	HiddenResponseHeaders = map[string]struct{}{
		"set-cookie": {},
	}

	// Formatted with http.CanonicalHeaderKey
	RequestIDHeaderKey  = "X-Request-Id"
	RequestIDContextKey = "slog-gin.request-id"
)

type Config struct {
	DefaultLevel     slog.Level
	ClientErrorLevel slog.Level
	ServerErrorLevel slog.Level

	WithUserAgent      bool
	WithRequestID      bool
	WithRequestBody    bool
	WithRequestHeader  bool
	WithResponseBody   bool
	WithResponseHeader bool
	WithSpanID         bool
	WithTraceID        bool

	HandleGinDebug bool

	Filters []Filter
}

// New returns a gin.HandlerFunc (middleware) that logs requests using slog.
//
// Requests with errors are logged using slog.Error().
// Requests without errors are logged using slog.Info().
func New(logger *slog.Logger) gin.HandlerFunc {
	return NewWithConfig(logger, Config{
		DefaultLevel:     slog.LevelInfo,
		ClientErrorLevel: slog.LevelWarn,
		ServerErrorLevel: slog.LevelError,

		WithUserAgent:      false,
		WithRequestID:      true,
		WithRequestBody:    false,
		WithRequestHeader:  false,
		WithResponseBody:   false,
		WithResponseHeader: false,
		WithSpanID:         false,
		WithTraceID:        false,

		HandleGinDebug: false,

		Filters: []Filter{},
	})
}

// NewWithFilters returns a gin.HandlerFunc (middleware) that logs requests using slog.
//
// Requests with errors are logged using slog.Error().
// Requests without errors are logged using slog.Info().
func NewWithFilters(logger *slog.Logger, filters ...Filter) gin.HandlerFunc {
	return NewWithConfig(logger, Config{
		DefaultLevel:     slog.LevelInfo,
		ClientErrorLevel: slog.LevelWarn,
		ServerErrorLevel: slog.LevelError,

		WithUserAgent:      false,
		WithRequestID:      true,
		WithRequestBody:    false,
		WithRequestHeader:  false,
		WithResponseBody:   false,
		WithResponseHeader: false,
		WithSpanID:         false,
		WithTraceID:        false,

		HandleGinDebug: false,

		Filters: filters,
	})
}

// NewWithConfig returns a gin.HandlerFunc (middleware) that logs requests using slog.
func NewWithConfig(logger *slog.Logger, config Config) gin.HandlerFunc {
	if config.HandleGinDebug {
		SetDebugPrintRouteFunc(logger)
		SetDebugPrintFunc(logger)
	}

	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		params := map[string]string{}
		for _, p := range c.Params {
			params[p.Key] = p.Value
		}

		requestID := c.GetHeader(RequestIDHeaderKey)
		if config.WithRequestID {
			if requestID == "" {
				requestID = uuid.New().String()
				c.Header(RequestIDHeaderKey, requestID)
			}
			c.Set(RequestIDContextKey, requestID)
		}

		// dump request body
		br := newBodyReader(c.Request.Body, RequestBodyMaxSize, config.WithRequestBody)
		c.Request.Body = br

		// dump response body
		bw := newBodyWriter(c.Writer, ResponseBodyMaxSize, config.WithResponseBody)
		c.Writer = bw

		c.Next()

		// Pass thru filters and skip early the code below, to prevent unnecessary processing.
		for _, filter := range config.Filters {
			if !filter(c) {
				return
			}
		}

		status := c.Writer.Status()
		method := c.Request.Method
		host := c.Request.Host
		route := c.FullPath()
		end := time.Now()
		latency := end.Sub(start)
		userAgent := c.Request.UserAgent()
		ip := c.ClientIP()
		referer := c.Request.Referer()

		baseAttributes := []slog.Attr{}

		requestAttributes := []slog.Attr{
			slog.Time("time", start.UTC()),
			slog.String("method", method),
			slog.String("host", host),
			slog.String("path", path),
			slog.String("query", query),
			slog.Any("params", params),
			slog.String("route", route),
			slog.String("ip", ip),
			slog.String("referer", referer),
		}

		responseAttributes := []slog.Attr{
			slog.Time("time", end.UTC()),
			slog.Duration("latency", latency),
			slog.Int("status", status),
		}

		if config.WithRequestID {
			baseAttributes = append(baseAttributes, slog.String(RequestIDKey, requestID))
		}

		// otel
		baseAttributes = append(baseAttributes, extractTraceSpanID(c.Request.Context(), config.WithTraceID, config.WithSpanID)...)

		// request body
		requestAttributes = append(requestAttributes, slog.Int("length", br.bytes))
		if config.WithRequestBody {
			requestAttributes = append(requestAttributes, slog.String("body", br.body.String()))
		}

		// request headers
		if config.WithRequestHeader {
			kv := []any{}

			for k, v := range c.Request.Header {
				if _, found := HiddenRequestHeaders[strings.ToLower(k)]; found {
					continue
				}
				kv = append(kv, slog.Any(k, v))
			}

			requestAttributes = append(requestAttributes, slog.Group("header", kv...))
		}

		if config.WithUserAgent {
			requestAttributes = append(requestAttributes, slog.String("user-agent", userAgent))
		}

		// response body
		responseAttributes = append(responseAttributes, slog.Int("length", bw.bytes))
		if config.WithResponseBody {
			responseAttributes = append(responseAttributes, slog.String("body", bw.body.String()))
		}

		// response headers
		if config.WithResponseHeader {
			kv := []any{}

			for k, v := range c.Writer.Header() {
				if _, found := HiddenResponseHeaders[strings.ToLower(k)]; found {
					continue
				}
				kv = append(kv, slog.Any(k, v))
			}

			responseAttributes = append(responseAttributes, slog.Group("header", kv...))
		}

		attributes := append(
			[]slog.Attr{
				{
					Key:   "request",
					Value: slog.GroupValue(requestAttributes...),
				},
				{
					Key:   "response",
					Value: slog.GroupValue(responseAttributes...),
				},
			},
			baseAttributes...,
		)

		// custom context values
		if v, ok := c.Get(customAttributesCtxKey); ok {
			switch attrs := v.(type) {
			case []slog.Attr:
				attributes = append(attributes, attrs...)
			}
		}

		level := config.DefaultLevel
		msg := "Incoming request"
		if status >= http.StatusBadRequest && status < http.StatusInternalServerError {
			level = config.ClientErrorLevel
			msg = strings.TrimSuffix(c.Errors.String(), "\n")
			if msg == "" {
				msg = fmt.Sprintf("HTTP error: %d %s", status, strings.ToLower(http.StatusText(status)))
			}
		} else if status >= http.StatusInternalServerError {
			level = config.ServerErrorLevel
			msg = strings.TrimSuffix(c.Errors.String(), "\n")
			if msg == "" {
				msg = fmt.Sprintf("HTTP error: %d %s", status, strings.ToLower(http.StatusText(status)))
			}
		}

		logger.LogAttrs(c.Request.Context(), level, msg, attributes...)
	}
}

// GetRequestID returns the request identifier.
func GetRequestID(c *gin.Context) string {
	requestID, ok := c.Get(RequestIDContextKey)
	if !ok {
		return ""
	}

	if id, ok := requestID.(string); ok {
		return id
	}

	return ""
}

// AddCustomAttributes adds custom attributes to the request context.
func AddCustomAttributes(c *gin.Context, attrs ...slog.Attr) {
	v, exists := c.Get(customAttributesCtxKey)
	if !exists {
		c.Set(customAttributesCtxKey, attrs)
		return
	}

	switch vAttrs := v.(type) {
	case []slog.Attr:
		c.Set(customAttributesCtxKey, append(vAttrs, attrs...))
	}
}

func extractTraceSpanID(ctx context.Context, withTraceID bool, withSpanID bool) []slog.Attr {
	if !withTraceID && !withSpanID {
		return []slog.Attr{}
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return []slog.Attr{}
	}

	attrs := []slog.Attr{}
	spanCtx := span.SpanContext()

	if withTraceID && spanCtx.HasTraceID() {
		traceID := trace.SpanFromContext(ctx).SpanContext().TraceID().String()
		attrs = append(attrs, slog.String(TraceIDKey, traceID))
	}

	if withSpanID && spanCtx.HasSpanID() {
		spanID := spanCtx.SpanID().String()
		attrs = append(attrs, slog.String(SpanIDKey, spanID))
	}

	return attrs
}
