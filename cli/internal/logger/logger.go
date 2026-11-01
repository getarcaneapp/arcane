// Package logger configures shared CLI logging.
package logger

import (
	"io"

	charmLog "charm.land/log/v2"

	"kit.getarcane.app"

	"getarcane.dev/cli/v3/internal/output"
)

// Setup configures the global logger level and format, defaulting invalid levels to info.
func Setup(w io.Writer, level string, jsonFormat bool) {
	lvl, err := charmLog.ParseLevel(level)
	if err != nil {
		lvl = charmLog.InfoLevel
	}

	log := charmLog.Default()
	log.SetLevel(lvl)
	log.SetOutput(output.CoordinatedWriter(w))
	log.SetReportTimestamp(true)

	log.SetFormatter(kit.Ternary(jsonFormat, charmLog.JSONFormatter, charmLog.TextFormatter))
}
