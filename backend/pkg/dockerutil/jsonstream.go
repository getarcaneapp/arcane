package docker

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/moby/moby/api/types/jsonstream"
)

// ConsumeJSONMessageStream drains a Docker JSON message stream and returns any daemon-reported error.
// Optional lineHandler receives each raw line before parsing.
func ConsumeJSONMessageStream(reader io.Reader, lineHandler func([]byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if lineHandler != nil {
			if err := lineHandler(line); err != nil {
				return err
			}
		}

		var msg jsonstream.Message
		if err := json.Unmarshal(line, &msg); err != nil {
			// Keep behavior resilient to any non-JSON line noise.
			continue
		}
		if msg.Error != nil {
			return msg.Error
		}

		// Some daemons include an additional top-level "error" string.
		var legacy struct {
			Error string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(line, &legacy); err == nil && strings.TrimSpace(legacy.Error) != "" {
			return errors.New(strings.TrimSpace(legacy.Error))
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}
