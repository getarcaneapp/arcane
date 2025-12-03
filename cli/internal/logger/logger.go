// Package logger provides logging configuration and access for the CLI.
//
// This package wraps logrus to provide consistent logging across the CLI.
// It supports configurable log levels and JSON output format for machine
// parsing.
//
// # Setup
//
// Call Setup early in the application lifecycle:
//
//	logger.Setup("debug", false) // debug level, text format
//	logger.Setup("info", true)   // info level, JSON format
//
// # Usage
//
//	log := logger.GetLogger()
//	log.Debug("detailed information")
//	log.Info("general information")
//	log.Error("error occurred")
package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

// Setup configures the global logger with the specified level and format.
// Valid log levels are: debug, info, warn, error, fatal, panic.
// If an invalid level is provided, it defaults to info.
// When jsonFormat is true, logs are output as JSON for machine parsing.
func Setup(level string, jsonFormat bool) {
	// Set the log level
	lvl, err := logrus.ParseLevel(level)
	if err != nil {
		lvl = logrus.InfoLevel
	}
	logrus.SetLevel(lvl)

	// Set the output to stdout
	logrus.SetOutput(os.Stdout)

	// Set the formatter
	if jsonFormat {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logrus.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
		})
	}
}

// GetLogger returns the global logger instance.
// The logger should be configured via Setup before use.
// This returns the logrus standard logger which can be used for
// Debug, Info, Warn, Error, Fatal, and Panic level logging.
func GetLogger() *logrus.Logger {
	return logrus.StandardLogger()
}
