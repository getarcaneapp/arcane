package utils

import (
	"log/slog"
	"os"
	"strings"
)

// ReadEnvFile returns the contents of the file named by NAME__FILE or
// NAME_FILE, double underscore first. It reports false when neither is set
// or the file cannot be read, which is logged.
func ReadEnvFile(name string) ([]byte, bool) {
	for _, suffix := range []string{"__FILE", "_FILE"} {
		filePath := os.Getenv(name + suffix)
		if filePath == "" {
			continue
		}

		// Docker secret paths are arbitrary absolute paths on the host, so no
		// confinement root exists for them.
		content, err := os.ReadFile(filePath) //nolint:gosec // file path intentionally comes from *_FILE env vars for Docker secrets
		if err != nil {
			slog.Warn("Failed to read secret from file, falling back to direct env var",
				"env", name+suffix, "error", err)
			return nil, false
		}

		return content, true
	}

	return nil, false
}

// LookupEnvOrFile is os.LookupEnv with Docker secret support: a readable
// NAME__FILE or NAME_FILE stands in for NAME, trimmed.
func LookupEnvOrFile(name string) (string, bool) {
	if content, ok := ReadEnvFile(name); ok {
		return strings.TrimSpace(string(content)), true
	}

	return os.LookupEnv(name)
}
