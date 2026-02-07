package ws

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeContainerLine(t *testing.T) {
	tests := []struct {
		name          string
		raw           string
		expectedLevel string
		expectedMsg   string
		expectedTS    string
	}{
		{
			name:          "plain stdout line",
			raw:           "hello world",
			expectedLevel: "stdout",
			expectedMsg:   "hello world",
			expectedTS:    "",
		},
		{
			name:          "stderr prefix",
			raw:           "[STDERR] error occurred",
			expectedLevel: "stderr",
			expectedMsg:   "error occurred",
			expectedTS:    "",
		},
		{
			name:          "stderr colon prefix",
			raw:           "stderr:error message",
			expectedLevel: "stderr",
			expectedMsg:   "error message",
			expectedTS:    "",
		},
		{
			name:          "stdout colon prefix",
			raw:           "stdout:normal message",
			expectedLevel: "stdout",
			expectedMsg:   "normal message",
			expectedTS:    "",
		},
		{
			name:          "trailing newline stripped",
			raw:           "message with newline\n",
			expectedLevel: "stdout",
			expectedMsg:   "message with newline",
			expectedTS:    "",
		},
		{
			name:          "trailing carriage return and newline",
			raw:           "message with crlf\r\n",
			expectedLevel: "stdout",
			expectedMsg:   "message with crlf",
			expectedTS:    "",
		},
		{
			name:          "multiple trailing newlines",
			raw:           "multiple newlines\n\n\n",
			expectedLevel: "stdout",
			expectedMsg:   "multiple newlines",
			expectedTS:    "",
		},
		{
			name:          "Docker timestamp RFC3339Nano",
			raw:           "2024-01-15T10:30:45.123456789Z hello from container",
			expectedLevel: "stdout",
			expectedMsg:   "hello from container",
			expectedTS:    "2024-01-15T10:30:45.123456789Z",
		},
		{
			name:          "Docker timestamp RFC3339",
			raw:           "2024-01-15T10:30:45Z hello from container",
			expectedLevel: "stdout",
			expectedMsg:   "hello from container",
			expectedTS:    "2024-01-15T10:30:45Z",
		},
		{
			name:          "stderr with Docker timestamp",
			raw:           "[STDERR] 2024-06-01T12:00:00.000Z critical error",
			expectedLevel: "stderr",
			expectedMsg:   "critical error",
			expectedTS:    "2024-06-01T12:00:00Z",
		},
		{
			name:          "empty line",
			raw:           "",
			expectedLevel: "stdout",
			expectedMsg:   "",
			expectedTS:    "",
		},
		{
			name:          "only whitespace",
			raw:           "   \n\r\n",
			expectedLevel: "stdout",
			expectedMsg:   "",
			expectedTS:    "",
		},
		{
			name:          "short line not mistaken for timestamp",
			raw:           "short",
			expectedLevel: "stdout",
			expectedMsg:   "short",
			expectedTS:    "",
		},
		{
			name:          "numeric start but not timestamp",
			raw:           "123 this is not a timestamp but starts with numbers and is long enough to check",
			expectedLevel: "stdout",
			expectedMsg:   "123 this is not a timestamp but starts with numbers and is long enough to check",
			expectedTS:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, msg, ts := NormalizeContainerLine(tt.raw)
			assert.Equal(t, tt.expectedLevel, level, "level mismatch")
			assert.Equal(t, tt.expectedMsg, msg, "message mismatch")
			assert.Equal(t, tt.expectedTS, ts, "timestamp mismatch")
		})
	}
}

func TestNormalizeProjectLine(t *testing.T) {
	tests := []struct {
		name            string
		raw             string
		expectedLevel   string
		expectedService string
		expectedMsg     string
		expectedTS      string
	}{
		{
			name:            "service pipe pattern",
			raw:             "web | Starting server on port 8080",
			expectedLevel:   "stdout",
			expectedService: "web",
			expectedMsg:     "Starting server on port 8080",
			expectedTS:      "",
		},
		{
			name:            "no service pattern",
			raw:             "plain log line without service",
			expectedLevel:   "stdout",
			expectedService: "",
			expectedMsg:     "plain log line without service",
			expectedTS:      "",
		},
		{
			name:            "stderr with service",
			raw:             "[STDERR] database | Connection refused",
			expectedLevel:   "stderr",
			expectedService: "database",
			expectedMsg:     "Connection refused",
			expectedTS:      "",
		},
		{
			name:            "service with timestamp",
			raw:             "2024-01-15T10:30:45.123Z api | Request received",
			expectedLevel:   "stdout",
			expectedService: "api",
			expectedMsg:     "Request received",
			expectedTS:      "2024-01-15T10:30:45.123Z",
		},
		{
			name:            "pipe in message without service format",
			raw:             "this | has | multiple | pipes",
			expectedLevel:   "stdout",
			expectedService: "this",
			expectedMsg:     "has | multiple | pipes",
			expectedTS:      "",
		},
		{
			name:            "service with whitespace padding",
			raw:             "  redis  | PING received",
			expectedLevel:   "stdout",
			expectedService: "redis",
			expectedMsg:     "PING received",
			expectedTS:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, service, msg, ts := NormalizeProjectLine(tt.raw)
			assert.Equal(t, tt.expectedLevel, level, "level mismatch")
			assert.Equal(t, tt.expectedService, service, "service mismatch")
			assert.Equal(t, tt.expectedMsg, msg, "message mismatch")
			assert.Equal(t, tt.expectedTS, ts, "timestamp mismatch")
		})
	}
}

func TestNowRFC3339(t *testing.T) {
	before := time.Now().UTC()
	result := NowRFC3339()
	after := time.Now().UTC()

	parsed, err := time.Parse(time.RFC3339Nano, result)
	require.NoError(t, err, "NowRFC3339 should return a valid RFC3339Nano string")

	assert.False(t, parsed.Before(before.Add(-time.Millisecond)),
		"timestamp should not be before the call")
	assert.False(t, parsed.After(after.Add(time.Millisecond)),
		"timestamp should not be after the call")
}
