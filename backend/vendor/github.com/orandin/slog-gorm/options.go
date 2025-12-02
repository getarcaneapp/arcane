package slogGorm

import (
	"context"
	"log/slog"
	"time"
)

type Option func(l *logger)

// WithLogger defines a custom logger to use
//
// Deprecated: Use WithHandler instead
func WithLogger(log *slog.Logger) Option {
	return func(l *logger) {
		if log != nil {
			l.sloggerHandler = log.Handler()
		}
	}
}

// WithHandler defines a custom logger to use
func WithHandler(handler slog.Handler) Option {
	return func(l *logger) {
		l.sloggerHandler = handler
	}
}

// WithSourceField defines the field to set the file name and line number of the current file
func WithSourceField(field string) Option {
	return func(l *logger) {
		l.sourceField = field
	}
}

// WithErrorField defines the field to set the error
func WithErrorField(field string) Option {
	return func(l *logger) {
		l.errorField = field
	}
}

// WithSlowThreshold defines the threshold above which a sql query is considered slow
func WithSlowThreshold(threshold time.Duration) Option {
	return func(l *logger) {
		l.slowThreshold = threshold
	}
}

// WithTraceAll enables mode which logs all SQL messages.
func WithTraceAll() Option {
	return func(l *logger) {
		l.traceAll = true
	}
}

// SetLogLevel sets a new slog.Level for a LogType.
func SetLogLevel(key LogType, level slog.Level) Option {
	return func(l *logger) {
		l.logLevel[key] = level
	}
}

// WithRecordNotFoundError allows the slogger to log gorm.ErrRecordNotFound errors
func WithRecordNotFoundError() Option {
	return func(l *logger) {
		l.ignoreRecordNotFoundError = false
	}
}

// WithIgnoreTrace disables the tracing of SQL queries by the slogger
func WithIgnoreTrace() Option {
	return func(l *logger) {
		l.ignoreTrace = true
	}
}

// WithContextValue adds a context value to the log
func WithContextValue(slogAttrName string, contextKey any) Option {
	return func(l *logger) {
		if l.contextKeys == nil {
			l.contextKeys = make(map[string]any, 0)
		}
		l.contextKeys[slogAttrName] = contextKey
	}
}

// WithContextFunc adds an attribute with the given name and slog.Value returned by the given
// function if the function returns true. No attribute will be added if the function returns false.
// Use this over WithContextValue if your context keys are not strings or only accessible via
// functions.
func WithContextFunc(slogAttrName string, slogValueFunc func(ctx context.Context) (slog.Value, bool)) Option {
	return func(l *logger) {
		if l.contextFuncs == nil {
			l.contextFuncs = make(map[string]func(context.Context) (slog.Value, bool))
		}
		l.contextFuncs[slogAttrName] = slogValueFunc
	}
}
