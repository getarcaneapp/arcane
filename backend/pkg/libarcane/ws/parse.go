package ws

import (
	"regexp"
	"strings"
	"time"
)

// Docker's RFC3339 timestamp when timestamps=true
var dockerTimestamp = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)\s+`)

// stripStreamMetadataInternal removes one leading Arcane stderr marker and one
// leading Docker RFC3339 timestamp (in either order) from the front of line.
// It reports whether the marker was removed and, when allowTimestamp is true
// and a valid Docker timestamp was found, its normalized value. The transport
// adds at most one marker and one timestamp per line, so anything past the
// first of each is application content and stays; timestamps that fail to
// parse are left in place.
func stripStreamMetadataInternal(line string, allowTimestamp bool) (rest string, sawStderr bool, timestamp string) {
	for {
		line = strings.TrimLeft(line, " \t")

		if !sawStderr && strings.HasPrefix(line, "[STDERR] ") {
			sawStderr = true
			line = line[len("[STDERR] "):]
			continue
		}

		if allowTimestamp && timestamp == "" && len(line) > 20 && line[0] >= '0' && line[0] <= '9' {
			if loc := dockerTimestamp.FindStringSubmatchIndex(line); loc != nil {
				if parsed, err := time.Parse(time.RFC3339Nano, line[loc[2]:loc[3]]); err == nil {
					timestamp = parsed.UTC().Format(time.RFC3339Nano)
					line = line[loc[1]:]
					continue
				}
			}
		}
		break
	}

	return line, sawStderr, timestamp
}

func trimTrailingNewlinesInternal(raw string) string {
	end := len(raw)
	for end > 0 {
		c := raw[end-1]
		if c != '\n' && c != '\r' {
			break
		}
		end--
	}
	return raw[:end]
}

// NormalizeContainerLine parses a raw container log line into level + cleaned message.
// It extracts Docker's timestamp if present (when timestamps=true in Docker API).
func NormalizeContainerLine(raw string) (level string, msg string, timestamp string) {
	level = "stdout"

	line, sawStderr, ts := stripStreamMetadataInternal(trimTrailingNewlinesInternal(raw), true)
	if sawStderr {
		level = "stderr"
	}
	if ts != "" {
		timestamp = ts
	}

	return level, strings.TrimSpace(line), timestamp
}

// NormalizeProjectLine additionally extracts service (pattern: service | message).
// It normalizes markers and Docker timestamps both before and after splitting
// the service prefix so either ordering resolves; stderr classification wins
// if either pass detects it, and the first valid Docker timestamp found is
// used and removed from the message. Application-generated timestamps deeper
// inside the message are preserved.
func NormalizeProjectLine(raw string) (level, service, msg, timestamp string) {
	level = "stdout"
	service = ""

	head, sawHeadStderr, headTS := stripStreamMetadataInternal(trimTrailingNewlinesInternal(raw), true)
	timestamp = headTS

	base := head
	if parts := strings.SplitN(base, " | ", 2); len(parts) == 2 {
		service = strings.TrimSpace(parts[0])

		message, sawMessageStderr, messageTS := stripStreamMetadataInternal(parts[1], headTS == "")
		if sawMessageStderr || sawHeadStderr {
			level = "stderr"
		}
		if messageTS != "" {
			timestamp = messageTS
		}
		base = message
	} else if sawHeadStderr {
		level = "stderr"
	}

	return level, service, strings.TrimSpace(base), timestamp
}

func NowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
