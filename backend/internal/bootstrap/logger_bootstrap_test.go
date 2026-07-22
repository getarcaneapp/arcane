package bootstrap

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/stretchr/testify/assert"
)

func TestRequestLogHandler(t *testing.T) {
	t.Run("condenses slog-echo records", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(&requestLogHandler{
			handler: slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		})

		logger.LogAttrs(t.Context(), slog.LevelWarn, "Not Found",
			slog.Group("request",
				slog.Time("time", time.Now()),
				slog.String("method", "GET"),
				slog.String("host", "localhost:3552"),
				slog.String("path", "/api/missing"),
				slog.String("query", ""),
				slog.Any("params", map[string]string{"id": "123"}),
				slog.String("route", ""),
				slog.String("referer", "https://example.com"),
				slog.Int("length", 0),
			),
			slog.Group("response",
				slog.Time("time", time.Now()),
				slog.Duration("latency", 1500*time.Microsecond),
				slog.Int("status", 404),
				slog.Int("length", 24),
			),
			slog.Any("error", map[string]any{
				"code":     404,
				"message":  "Not Found",
				"internal": nil,
			}),
		)

		output := buf.String()
		assert.Contains(t, output, "method=GET")
		assert.Contains(t, output, "status=404")
		assert.Contains(t, output, "duration=")
		assert.Contains(t, output, `error="Not Found"`)
		assert.NotContains(t, output, "request.params")
		assert.NotContains(t, output, "request.referer")
		assert.NotContains(t, output, "map[code:")
	})

	t.Run("passes plain records through unchanged", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(&requestLogHandler{
			handler: slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
		})

		logger.InfoContext(t.Context(), "plain record", "component", "scheduler")

		assert.Contains(t, buf.String(), "msg=\"plain record\" component=scheduler")
	})
}

func TestStackAttrHandlerAddsStackToErrorRecords(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(&stackAttrHandler{
		handler: slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	})
	err := errors.New("boom")

	logger.InfoContext(t.Context(), "info", "error", err)
	assert.NotContains(t, buf.String(), "stack=")

	logger.ErrorContext(t.Context(), "failed", "err", err)
	assert.Contains(t, buf.String(), "stack=")
}
