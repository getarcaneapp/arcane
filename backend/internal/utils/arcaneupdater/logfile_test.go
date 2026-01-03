package arcaneupdater

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestMessageOnlyHandler_Handle(t *testing.T) {
	tests := []struct {
		name    string
		level   slog.Level
		message string
		attrs   []slog.Attr
		want    string
	}{
		{
			name:    "info message only",
			level:   slog.LevelInfo,
			message: "Test message",
			attrs:   []slog.Attr{},
			want:    "Test message",
		},
		{
			name:    "debug message",
			level:   slog.LevelDebug,
			message: "Debug info",
			attrs:   []slog.Attr{},
			want:    "Debug info",
		},
		{
			name:    "error message",
			level:   slog.LevelError,
			message: "Error occurred",
			attrs:   []slog.Attr{},
			want:    "Error occurred",
		},
		{
			name:    "message with string attribute",
			level:   slog.LevelInfo,
			message: "Processing",
			attrs:   []slog.Attr{slog.String("key", "value")},
			want:    "Processing key=\"value\"",
		},
		{
			name:    "message with int attribute",
			level:   slog.LevelInfo,
			message: "Count",
			attrs:   []slog.Attr{slog.Int("count", 42)},
			want:    "Count count=42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			handler := newMessageOnlyHandler(&buf, slog.LevelDebug)

			record := slog.NewRecord(time.Now(), tt.level, tt.message, 0)
			for _, attr := range tt.attrs {
				record.AddAttrs(attr)
			}

			if err := handler.Handle(context.Background(), record); err != nil {
				t.Errorf("Handle() error = %v", err)
				return
			}

			got := strings.TrimSpace(buf.String())
			if got != tt.want {
				t.Errorf("Handle() output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMessageOnlyHandler_Enabled(t *testing.T) {
	tests := []struct {
		name     string
		minLevel slog.Level
		level    slog.Level
		want     bool
	}{
		{
			name:     "info enabled at debug level",
			minLevel: slog.LevelDebug,
			level:    slog.LevelInfo,
			want:     true,
		},
		{
			name:     "debug disabled at info level",
			minLevel: slog.LevelInfo,
			level:    slog.LevelDebug,
			want:     false,
		},
		{
			name:     "error enabled at info level",
			minLevel: slog.LevelInfo,
			level:    slog.LevelError,
			want:     true,
		},
		{
			name:     "warn enabled at warn level",
			minLevel: slog.LevelWarn,
			level:    slog.LevelWarn,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			handler := newMessageOnlyHandler(&buf, tt.minLevel)

			if got := handler.Enabled(context.Background(), tt.level); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessageOnlyHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	handler := newMessageOnlyHandler(&buf, slog.LevelInfo)

	attrs := []slog.Attr{
		slog.String("key1", "value1"),
		slog.Int("key2", 42),
	}

	newHandler := handler.WithAttrs(attrs)
	if newHandler == nil {
		t.Error("WithAttrs() returned nil")
	}

	// Should return same handler type
	if _, ok := newHandler.(*messageOnlyHandler); !ok {
		t.Error("WithAttrs() did not return messageOnlyHandler")
	}

	// Test that attrs are included in output
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "Test", 0)
	if err := newHandler.Handle(context.Background(), record); err != nil {
		t.Errorf("Handle() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "key1") || !strings.Contains(output, "key2") {
		t.Errorf("WithAttrs() output missing attributes: %q", output)
	}
}

func TestMessageOnlyHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	handler := newMessageOnlyHandler(&buf, slog.LevelInfo)

	newHandler := handler.WithGroup("testgroup")
	if newHandler == nil {
		t.Error("WithGroup() returned nil")
	}

	// Should return same handler type
	if _, ok := newHandler.(*messageOnlyHandler); !ok {
		t.Error("WithGroup() did not return messageOnlyHandler")
	}

	// Test that group is used in output
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "Test", 0)
	record.AddAttrs(slog.String("key", "value"))
	if err := newHandler.Handle(context.Background(), record); err != nil {
		t.Errorf("Handle() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "testgroup") {
		t.Errorf("WithGroup() output missing group name: %q", output)
	}
}

func TestFormatSlogValue(t *testing.T) {
	tests := []struct {
		name  string
		value slog.Value
		want  string
	}{
		{
			name:  "string value",
			value: slog.StringValue("test"),
			want:  "\"test\"",
		},
		{
			name:  "int value",
			value: slog.Int64Value(42),
			want:  "42",
		},
		{
			name:  "bool value true",
			value: slog.BoolValue(true),
			want:  "true",
		},
		{
			name:  "bool value false",
			value: slog.BoolValue(false),
			want:  "false",
		},
		{
			name:  "float value",
			value: slog.Float64Value(3.14),
			want:  "3.14",
		},
		{
			name:  "uint value",
			value: slog.Uint64Value(100),
			want:  "100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSlogValue(tt.value)
			if got != tt.want {
				t.Errorf("formatSlogValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatSlogValue_Time(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	value := slog.TimeValue(now)

	got := formatSlogValue(value)

	// Should be quoted and contain the date
	if !strings.HasPrefix(got, "\"") || !strings.HasSuffix(got, "\"") {
		t.Errorf("formatSlogValue(time) should be quoted, got %q", got)
	}
	if !strings.Contains(got, "2024") {
		t.Errorf("formatSlogValue(time) should contain year, got %q", got)
	}
}

func TestFormatSlogValue_Duration(t *testing.T) {
	value := slog.DurationValue(5 * time.Second)
	got := formatSlogValue(value)

	// Should be quoted and contain duration string
	if !strings.HasPrefix(got, "\"") || !strings.HasSuffix(got, "\"") {
		t.Errorf("formatSlogValue(duration) should be quoted, got %q", got)
	}
	if !strings.Contains(got, "5s") {
		t.Errorf("formatSlogValue(duration) should contain duration, got %q", got)
	}
}

func TestTeeHandler_Enabled(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	handler1 := newMessageOnlyHandler(&buf1, slog.LevelInfo)
	handler2 := newMessageOnlyHandler(&buf2, slog.LevelWarn)

	tee := teeHandler{a: handler1, b: handler2}

	tests := []struct {
		name  string
		level slog.Level
		want  bool
	}{
		{
			name:  "debug - neither enabled",
			level: slog.LevelDebug,
			want:  false,
		},
		{
			name:  "info - first enabled",
			level: slog.LevelInfo,
			want:  true,
		},
		{
			name:  "warn - both enabled",
			level: slog.LevelWarn,
			want:  true,
		},
		{
			name:  "error - both enabled",
			level: slog.LevelError,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tee.Enabled(context.Background(), tt.level); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTeeHandler_Handle(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	handler1 := newMessageOnlyHandler(&buf1, slog.LevelInfo)
	handler2 := newMessageOnlyHandler(&buf2, slog.LevelInfo)

	tee := teeHandler{a: handler1, b: handler2}

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "Test message", 0)
	if err := tee.Handle(context.Background(), record); err != nil {
		t.Errorf("Handle() error = %v", err)
	}

	// Both handlers should have received the message
	if !strings.Contains(buf1.String(), "Test message") {
		t.Error("teeHandler did not write to first handler")
	}
	if !strings.Contains(buf2.String(), "Test message") {
		t.Error("teeHandler did not write to second handler")
	}
}

func TestTeeHandler_WithAttrs(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	handler1 := newMessageOnlyHandler(&buf1, slog.LevelInfo)
	handler2 := newMessageOnlyHandler(&buf2, slog.LevelInfo)

	tee := teeHandler{a: handler1, b: handler2}

	attrs := []slog.Attr{slog.String("key", "value")}
	newTee := tee.WithAttrs(attrs)

	if _, ok := newTee.(teeHandler); !ok {
		t.Error("WithAttrs() did not return teeHandler")
	}
}

func TestTeeHandler_WithGroup(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	handler1 := newMessageOnlyHandler(&buf1, slog.LevelInfo)
	handler2 := newMessageOnlyHandler(&buf2, slog.LevelInfo)

	tee := teeHandler{a: handler1, b: handler2}

	newTee := tee.WithGroup("testgroup")

	if _, ok := newTee.(teeHandler); !ok {
		t.Error("WithGroup() did not return teeHandler")
	}
}

func TestNewMessageOnlyHandler(t *testing.T) {
	var buf bytes.Buffer
	handler := newMessageOnlyHandler(&buf, slog.LevelInfo)

	if handler.minLevel != slog.LevelInfo {
		t.Errorf("newMessageOnlyHandler() minLevel = %v, want %v", handler.minLevel, slog.LevelInfo)
	}

	if handler.mu == nil {
		t.Error("newMessageOnlyHandler() did not initialize mutex")
	}
}

func TestMessageOnlyHandler_MultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	handler := newMessageOnlyHandler(&buf, slog.LevelInfo)

	messages := []string{
		"First message",
		"Second message",
		"Third message",
	}

	for _, msg := range messages {
		record := slog.NewRecord(time.Now(), slog.LevelInfo, msg, 0)
		if err := handler.Handle(context.Background(), record); err != nil {
			t.Errorf("Handle() error = %v", err)
		}
	}

	output := buf.String()
	for _, msg := range messages {
		if !strings.Contains(output, msg) {
			t.Errorf("Output missing message %q", msg)
		}
	}
}
