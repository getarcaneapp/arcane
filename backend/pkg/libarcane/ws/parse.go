package ws

import (
	"regexp"
	"strings"
	"time"
)

// Docker's RFC3339 timestamp when timestamps=true
var dockerTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z\s+`)

// NormalizeContainerLine parses a raw container log line into level + cleaned message.
// It extracts Docker's timestamp if present (when timestamps=true in Docker API).
func NormalizeContainerLine(raw string) (level string, msg string, timestamp string) {
	// Fast trim right for common cases to avoid scanning the whole string
	end := len(raw)
	for end > 0 {
		c := raw[end-1]
		if c != '\n' && c != '\r' {
			break
		}
		end--
	}
	line := raw[:end]

	level = "stdout"
	// Check prefixes using slicing for performance
	switch {
	case strings.HasPrefix(line, "[STDERR] "):
		level = "stderr"
		line = line[9:]
	case strings.HasPrefix(line, "stderr:"):
		level = "stderr"
		line = line[7:]
	case strings.HasPrefix(line, "stdout:"):
		level = "stdout"
		line = line[7:]
	}

	// Extract and strip Docker's RFC3339 timestamp (when timestamps=true)
	// Optimization: Check if it looks like a timestamp before running regex
	if len(line) > 20 && line[0] >= '0' && line[0] <= '9' {
		if loc := dockerTimestamp.FindStringIndex(line); loc != nil {
			// loc[0] is start, loc[1] is end
			matchStr := line[loc[0]:loc[1]]
			trimmed := strings.TrimSpace(matchStr)

			if parsed, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
				timestamp = parsed.UTC().Format(time.RFC3339Nano)
			} else if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
				timestamp = parsed.UTC().Format(time.RFC3339Nano)
			}

			// Strip the timestamp from the line
			line = line[loc[1]:]
		}
	}

	// Return the message as-is (including any application-level timestamps)
	return level, strings.TrimSpace(line), timestamp
}

// NormalizeProjectLine additionally extracts service (pattern: service | message).
// Returns level, service, message, timestamp (RFC3339Nano) — timestamp may be empty.
func NormalizeProjectLine(raw string) (level, service, msg, timestamp string) {
	level, base, ts := NormalizeContainerLine(raw)
	timestamp = ts

	service = ""
	if parts := strings.SplitN(base, " | ", 2); len(parts) == 2 {
		service = strings.TrimSpace(parts[0])
		base = parts[1]
	}
	return level, service, base, timestamp
}

func NowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
