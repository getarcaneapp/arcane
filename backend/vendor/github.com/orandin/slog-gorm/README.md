# slog-gorm

[![Go Reference](https://pkg.go.dev/badge/github.com/orandin/slog-gorm.svg)](https://pkg.go.dev/github.com/orandin/slog-gorm)
![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.21-%23007d9c)
[![CI](https://github.com/orandin/slog-gorm/actions/workflows/ci.yaml/badge.svg)](https://github.com/orandin/slog-gorm/actions/workflows/ci.yaml)
[![Go report](https://goreportcard.com/badge/github.com/orandin/slog-gorm)](https://goreportcard.com/report/github.com/orandin/slog-gorm)
[![Coverage](https://img.shields.io/codecov/c/github/orandin/slog-gorm)](https://codecov.io/gh/orandin/slog-gorm)
[![Renovate](https://img.shields.io/badge/dependabot-enabled-brightgreen.svg)](https://docs.github.com/en/code-security/dependabot/working-with-dependabot)
[![License](https://img.shields.io/github/license/orandin/slog-gorm)](./LICENSE)

`slog-gorm` provides a slog adapter, highly configurable, for [gorm logger](https://gorm.io/docs/logger.html)
to have homogeneous logs between your application / script and gorm.

## Key features

- compatible with any `slog.Handler`, which allows you to keep control on
  the format of your logs.
- can define a threshold to identify and log the slow queries.
- can log all SQL messages or just the errors if you prefer.
- can define a custom `slog.Level` for errors, slow queries or the other logs.
- can log context values with each Gorm log.

## Requirement

- `golang >= 1.21`

## Usage

```golang
import (
    "log/slog"
    "os"

    "gorm.io/driver/sqlite"
    "gorm.io/gorm"

    slogGorm "github.com/orandin/slog-gorm"
)

// Create an slog-gorm instance
gormLogger := slogGorm.New() // use slog.Default() by default


// GORM: Globally mode
db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{
    Logger: gormLogger,
})

// GORM: Continuous session mode
tx := db.Session(&Session{Logger: gormLogger})
tx.First(&user)
tx.Model(&user).Update("Age", 18)
```

### With your `slog.Logger`

The following example shows you how to use a specific `slog.Logger` with `slog-gorm`:

```golang
// With your slog.Logger
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

// Also, you can set specific attributes to distinguish between your application logs and gorm logs
// logger = logger.With(slog.String("log_type", "database"))

gormLogger := slogGorm.New(
    // slogGorm.WithLogger(logger), // Deprecated since v1.3.0, use `slogGorm.WithHandler(...)` instead.
    slogGorm.WithHandler(logger.Handler()), // since v1.3.0
    slogGorm.WithTraceAll(), // trace all messages 
    slogGorm.SetLogLevel(DefaultLogType, slog.Level(32)), // Define the default logging level
)
```

### Use your custom `slog.Level`

As some loggers *(e.g. syslog)* have their own logging levels, `slog-gorm` lets you
use them to ensure the consistency of your logs and make them easier to understand.

You can set the logging level for these log types:

| Type                        | Description                          | Default           |
|-----------------------------|--------------------------------------|-------------------|
| `slogGorm.ErrorLogType`     | For SQL errors                       | `slog.LevelError` |
| `slogGorm.SlowQueryLogType` | For slow queries                     | `slog.LevelWarn`  |
| `slogGorm.DefaultLogType`   | For other messages *(default level)* | `slog.LevelInfo`  |

Example:

```golang
const (
    LOG_EMERG   = slog.Level(0)
    // ...
    LOG_ERR     = slog.Level(3)
    LOG_WARNING = slog.Level(4)
    LOG_NOTICE  = slog.Level(5)
    // ...
    LOG_DEBUG   = slog.Level(7)
)

logger := slog.New(syslogHandler)

gormLogger := slogGorm.New(
    slogGorm.WithHandler(logger.Handler()),

    // Set logging level for SQL errors
    slogGorm.SetLogLevel(slogGorm.ErrorLogType, LOG_ERR)

    // Set logging level for slow queries
    slogGorm.SetLogLevel(slogGorm.SlowQueryLogType, LOG_NOTICE)

    // Set logging level for other messages (default level)
    slogGorm.SetLogLevel(slogGorm.DefaultLogType, LOG_DEBUG)
)
```

### Other options

```golang
customLogger := sloggorm.New(
	slogGorm.WithSlowThreshold(500 * time.Millisecond), // to identify slow queries

	slogGorm.WithRecordNotFoundError(), // don't ignore not found errors

	slogGorm.WithSourceField("origin"), // instead of "file" (by default)

	slogGorm.WithErrorField("err"),     // instead of "error" (by default)

	slogGorm.WithContextValue("slogAttrName1", "ctxKey"), // adds an slog.Attr if a value is found for this key in the Gorm's query context

	slogGorm.WithContextFunc("slogAttrName2", func(ctx context.Context) (slog.Value, bool) {
		v, ok := ctx.Value(ctxKey1).(time.Duration)
		if !ok {
			return slog.Value{}, false
		}
		return slog.DurationValue(v), true
	}), // adds an slog.Attr if the given function returns an slog.Value and true
)
```

By default, the slow queries and SQL errors are logged, but you can ignore all SQL messages with `WithIgnoreTrace()`.

```
customLogger := sloggorm.New(
    slogGorm.WithIgnoreTrace(), // disable the tracing of SQL queries by the logger.
)
```
