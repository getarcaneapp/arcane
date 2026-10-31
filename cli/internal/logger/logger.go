// Package logger configures shared CLI logging.
package logger

import (
	"io"

	charmlog "charm.land/log/v2"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
)

// Setup configures the global logger level and format, defaulting invalid levels to info.
func Setup(w io.Writer, level string, jsonFormat bool) {
	lvl, err := charmlog.ParseLevel(level)
	if err != nil {
		lvl = charmlog.InfoLevel
	}

	log := charmlog.Default()
	log.SetLevel(lvl)
	log.SetOutput(output.CoordinatedWriter(w))
	log.SetReportTimestamp(true)

	log.SetFormatter(kit.Ternary(jsonFormat, charmlog.JSONFormatter, charmlog.TextFormatter))
}

// GetLogger returns the global logger instance.
// The logger should be configured via Setup before use.
func GetLogger() *charmlog.Logger {
	return charmlog.Default()
}
