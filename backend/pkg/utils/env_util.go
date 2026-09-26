package utils

import (
	"log/slog"
	"os"
	"strings"
)

// LookupEnvOrFile is os.LookupEnv with Docker secret support: when NAME__FILE
// or NAME_FILE (checked in that order) names a readable file, its trimmed
// contents stand in for NAME. An unreadable file falls back to NAME.
// The booleans report whether a value was found and whether it came from a file.
func LookupEnvOrFile(name string) (value string, found bool, fromFile bool) {
	for _, suffix := range []string{"__FILE", "_FILE"} {
		filePath := os.Getenv(name + suffix)
		if filePath == "" {
			continue
		}

		// Secret paths are arbitrary host paths, so there is no acfs root to confine them to.
		content, err := os.ReadFile(filePath) //nolint:gosec // path intentionally comes from a *_FILE env var
		if err != nil {
			slog.Warn("Failed to read secret file, falling back to direct env var", "env", name+suffix, "error", err)
			break
		}

		return strings.TrimSpace(string(content)), true, true
	}

	value, found = os.LookupEnv(name)
	return value, found, false
}
