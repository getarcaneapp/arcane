package utils

import (
	"log/slog"
	"os"
	"strings"
)

// LookupEnvOrFile is os.LookupEnv with Docker secret support: when NAME__FILE
// or NAME_FILE (checked in that order) names a readable file, its trimmed
// contents stand in for NAME. An unreadable file is logged and skipped.
func LookupEnvOrFile(name string) (string, bool) {
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

		return strings.TrimSpace(string(content)), true
	}

	return os.LookupEnv(name)
}
