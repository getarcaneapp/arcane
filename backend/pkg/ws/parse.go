package ws

import (
	"cmp"
	"regexp"
	"strings"
	"time"
)

// Docker's RFC3339 timestamp when timestamps=true
var dockerTimestamp = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)\s+`)

func stripStreamMetadata(line string, allowTimestamp bool) (rest string, sawStderr bool, timestamp string) {
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

func trimTrailingNewlines(raw string) string {
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

func NormalizeContainerLine(raw string) (level, msg, timestamp string) {
	level = "stdout"

	line, sawStderr, ts := stripStreamMetadata(trimTrailingNewlines(raw), true)
	if sawStderr {
		level = "stderr"
	}
	timestamp = cmp.Or(ts, timestamp)

	return level, strings.TrimSpace(line), timestamp
}

func NormalizeProjectLine(raw string) (level, service, msg, timestamp string) {
	level = "stdout"
	service = ""

	head, sawHeadStderr, headTS := stripStreamMetadata(trimTrailingNewlines(raw), true)
	timestamp = headTS

	base := head
	if parts := strings.SplitN(base, " | ", 2); len(parts) == 2 {
		service = strings.TrimSpace(parts[0])

		message, sawMessageStderr, messageTS := stripStreamMetadata(parts[1], headTS == "")
		if sawMessageStderr || sawHeadStderr {
			level = "stderr"
		}
		timestamp = cmp.Or(messageTS, timestamp)
		base = message
	} else if sawHeadStderr {
		level = "stderr"
	}

	return level, service, strings.TrimSpace(base), timestamp
}
