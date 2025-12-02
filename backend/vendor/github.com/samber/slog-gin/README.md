
# slog: Gin middleware

[![tag](https://img.shields.io/github/tag/samber/slog-gin.svg)](https://github.com/samber/slog-gin/releases)
![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.21-%23007d9c)
[![GoDoc](https://godoc.org/github.com/samber/slog-gin?status.svg)](https://pkg.go.dev/github.com/samber/slog-gin)
![Build Status](https://github.com/samber/slog-gin/actions/workflows/test.yml/badge.svg)
[![Go report](https://goreportcard.com/badge/github.com/samber/slog-gin)](https://goreportcard.com/report/github.com/samber/slog-gin)
[![Coverage](https://img.shields.io/codecov/c/github/samber/slog-gin)](https://codecov.io/gh/samber/slog-gin)
[![Contributors](https://img.shields.io/github/contributors/samber/slog-gin)](https://github.com/samber/slog-gin/graphs/contributors)
[![License](https://img.shields.io/github/license/samber/slog-gin)](./LICENSE)

[Gin](https://github.com/gin-gonic/gin) middleware to log http requests using [slog](https://pkg.go.dev/log/slog).

<div align="center">
  <hr>
  <sup><b>Sponsored by:</b></sup>
  <br>
  <a href="https://www.dash0.com?utm_campaign=148395251-samber%20github%20sponsorship&utm_source=github&utm_medium=sponsorship&utm_content=samber">
    <div>
      <img src="https://github.com/user-attachments/assets/b1f2e876-0954-4dc3-824d-935d29ba8f3f" width="200" alt="Dash0">
    </div>
    <div>
      100% OpenTelemetry-native observability platform<br>Simple to use, built on open standards, and designed for full cost control
    </div>
  </a>
  <hr>
</div>

**See also:**

- [slog-multi](https://github.com/samber/slog-multi): `slog.Handler` chaining, fanout, routing, failover, load balancing...
- [slog-formatter](https://github.com/samber/slog-formatter): `slog` attribute formatting
- [slog-sampling](https://github.com/samber/slog-sampling): `slog` sampling policy
- [slog-mock](https://github.com/samber/slog-mock): `slog.Handler` for test purposes

**HTTP middlewares:**

- [slog-gin](https://github.com/samber/slog-gin): Gin middleware for `slog` logger
- [slog-echo](https://github.com/samber/slog-echo): Echo middleware for `slog` logger
- [slog-fiber](https://github.com/samber/slog-fiber): Fiber middleware for `slog` logger
- [slog-chi](https://github.com/samber/slog-chi): Chi middleware for `slog` logger
- [slog-http](https://github.com/samber/slog-http): `net/http` middleware for `slog` logger

**Loggers:**

- [slog-zap](https://github.com/samber/slog-zap): A `slog` handler for `Zap`
- [slog-zerolog](https://github.com/samber/slog-zerolog): A `slog` handler for `Zerolog`
- [slog-logrus](https://github.com/samber/slog-logrus): A `slog` handler for `Logrus`

**Log sinks:**

- [slog-datadog](https://github.com/samber/slog-datadog): A `slog` handler for `Datadog`
- [slog-betterstack](https://github.com/samber/slog-betterstack): A `slog` handler for `Betterstack`
- [slog-rollbar](https://github.com/samber/slog-rollbar): A `slog` handler for `Rollbar`
- [slog-loki](https://github.com/samber/slog-loki): A `slog` handler for `Loki`
- [slog-sentry](https://github.com/samber/slog-sentry): A `slog` handler for `Sentry`
- [slog-syslog](https://github.com/samber/slog-syslog): A `slog` handler for `Syslog`
- [slog-logstash](https://github.com/samber/slog-logstash): A `slog` handler for `Logstash`
- [slog-fluentd](https://github.com/samber/slog-fluentd): A `slog` handler for `Fluentd`
- [slog-graylog](https://github.com/samber/slog-graylog): A `slog` handler for `Graylog`
- [slog-quickwit](https://github.com/samber/slog-quickwit): A `slog` handler for `Quickwit`
- [slog-slack](https://github.com/samber/slog-slack): A `slog` handler for `Slack`
- [slog-telegram](https://github.com/samber/slog-telegram): A `slog` handler for `Telegram`
- [slog-mattermost](https://github.com/samber/slog-mattermost): A `slog` handler for `Mattermost`
- [slog-microsoft-teams](https://github.com/samber/slog-microsoft-teams): A `slog` handler for `Microsoft Teams`
- [slog-webhook](https://github.com/samber/slog-webhook): A `slog` handler for `Webhook`
- [slog-kafka](https://github.com/samber/slog-kafka): A `slog` handler for `Kafka`
- [slog-nats](https://github.com/samber/slog-nats): A `slog` handler for `NATS`
- [slog-parquet](https://github.com/samber/slog-parquet): A `slog` handler for `Parquet` + `Object Storage`
- [slog-channel](https://github.com/samber/slog-channel): A `slog` handler for Go channels

## 🚀 Install

```sh
go get github.com/samber/slog-gin
```

**Compatibility**: go >= 1.21

No breaking changes will be made to exported APIs before v2.0.0.

## 💡 Usage

### Handler options

```go
type Config struct {
	DefaultLevel     slog.Level
	ClientErrorLevel slog.Level
	ServerErrorLevel slog.Level

	WithUserAgent      bool
	WithRequestID      bool
	WithRequestBody    bool
	WithRequestHeader  bool
	WithResponseBody   bool
	WithResponseHeader bool
	WithSpanID         bool
	WithTraceID        bool

	HandleGinDebug bool

	Filters []Filter
}
```

Attributes will be injected in log payload.

Other global parameters:

```go
sloggin.TraceIDKey = "trace_id"
sloggin.SpanIDKey = "span_id"
sloggin.RequestBodyMaxSize  = 64 * 1024 // 64KB
sloggin.ResponseBodyMaxSize = 64 * 1024 // 64KB
sloggin.HiddenRequestHeaders = map[string]struct{}{ ... }
sloggin.HiddenResponseHeaders = map[string]struct{}{ ... }
sloggin.RequestIDHeaderKey = "X-Request-Id"
sloggin.RequestIDContextKey = "slog-gin.request-id"
```

### Minimal

```go
import (
	"github.com/gin-gonic/gin"
	sloggin "github.com/samber/slog-gin"
	"log/slog"
)

// Create a slog logger, which:
//   - Logs to stdout.
logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

router := gin.New()

// Add the sloggin middleware to all routes.
// The middleware will log all requests attributes.
router.Use(sloggin.New(logger))
router.Use(gin.Recovery())

// Example pong request.
router.GET("/pong", func(c *gin.Context) {
    c.String(http.StatusOK, "pong")
})

router.Run(":1234")

// output:
// time=2023-10-15T20:32:58.926+02:00 level=INFO msg="Incoming request" env=production request.time=2023-10-15T20:32:58.626+02:00 request.method=GET request.path=/ request.query="" request.route="" request.ip=127.0.0.1:63932 request.length=0 response.time=2023-10-15T20:32:58.926+02:00 response.latency=100ms response.status=200 response.length=7 id=""
```

### OTEL

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

config := sloggin.Config{
	WithSpanID:  true,
	WithTraceID: true,
}

router := gin.New()
router.Use(sloggin.NewWithConfig(logger, config))
```

### Custom log levels

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

config := sloggin.Config{
	DefaultLevel:     slog.LevelInfo,
	ClientErrorLevel: slog.LevelWarn,
	ServerErrorLevel: slog.LevelError,
}

router := gin.New()
router.Use(sloggin.NewWithConfig(logger, config))
```

### Verbose

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

config := sloggin.Config{
	WithRequestBody: true,
	WithResponseBody: true,
	WithRequestHeader: true,
	WithResponseHeader: true,
}

router := gin.New()
router.Use(sloggin.NewWithConfig(logger, config))
```

### Filters

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

router := gin.New()
router.Use(
	sloggin.NewWithFilters(
		logger,
		sloggin.Accept(func (c *gin.Context) bool {
			return xxx
		}),
		sloggin.IgnoreStatus(401, 404),
	),
)
```

Available filters:
- Accept / Ignore
- AcceptMethod / IgnoreMethod
- AcceptStatus / IgnoreStatus
- AcceptStatusGreaterThan / IgnoreStatusGreaterThan
- AcceptStatusLessThan / IgnoreStatusLessThan
- AcceptStatusGreaterThanOrEqual / IgnoreStatusGreaterThanOrEqual
- AcceptStatusLessThanOrEqual / IgnoreStatusLessThanOrEqual
- AcceptPath / IgnorePath
- AcceptPathContains / IgnorePathContains
- AcceptPathPrefix / IgnorePathPrefix
- AcceptPathSuffix / IgnorePathSuffix
- AcceptPathMatch / IgnorePathMatch
- AcceptHost / IgnoreHost
- AcceptHostContains / IgnoreHostContains
- AcceptHostPrefix / IgnoreHostPrefix
- AcceptHostSuffix / IgnoreHostSuffix
- AcceptHostMatch / IgnoreHostMatch

### Using custom time formatters

```go
import (
	"github.com/gin-gonic/gin"
	sloggin "github.com/samber/slog-gin"
	slogformatter "github.com/samber/slog-formatter"
	"log/slog"
)

// Create a slog logger, which:
//   - Logs to stdout.
//   - RFC3339 with UTC time format.
logger := slog.New(
    slogformatter.NewFormatterHandler(
        slogformatter.TimezoneConverter(time.UTC),
        slogformatter.TimeFormatter(time.DateTime, nil),
    )(
        slog.NewTextHandler(os.Stdout, nil),
    ),
)

router := gin.New()

// Add the sloggin middleware to all routes.
// The middleware will log all requests attributes.
router.Use(sloggin.New(logger))
router.Use(gin.Recovery())

// Example pong request.
router.GET("/pong", func(c *gin.Context) {
    c.String(http.StatusOK, "pong")
})

router.Run(":1234")

// output:
// time=2023-10-15T20:32:58.926+02:00 level=INFO msg="Incoming request" env=production request.time=2023-10-15T20:32:58Z request.method=GET request.path=/ request.query="" request.route="" request.ip=127.0.0.1:63932 request.length=0 response.time=2023-10-15T20:32:58Z response.latency=100ms response.status=200 response.length=7 id=""
```

### Using custom logger sub-group

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

router := gin.New()

// Add the sloggin middleware to all routes.
// The middleware will log all requests attributes under a "http" group.
router.Use(sloggin.New(logger.WithGroup("http")))
router.Use(gin.Recovery())

// Example pong request.
router.GET("/pong", func(c *gin.Context) {
    c.String(http.StatusOK, "pong")
})

router.Run(":1234")

// output:
// time=2023-10-15T20:32:58.926+02:00 level=INFO msg="Incoming request" env=production http.request.time=2023-10-15T20:32:58.626+02:00 http.request.method=GET http.request.path=/ request.query="" http.request.route="" http.request.ip=127.0.0.1:63932 http.request.length=0 http.response.time=2023-10-15T20:32:58.926+02:00 http.response.latency=100ms http.response.status=200 http.response.length=7 http.id=""
```

### Add logger to a single route

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

router := gin.New()
router.Use(gin.Recovery())

// Example pong request.
// Add the sloggin middleware to a single routes.
router.GET("/pong", sloggin.New(logger), func(c *gin.Context) {
    c.String(http.StatusOK, "pong")
})

router.Run(":1234")
```

### Adding custom attributes

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, nil)).
    With("environment", "production").
    With("server", "gin/1.9.0").
    With("server_start_time", time.Now()).
    With("gin_mode", gin.EnvGinMode)

router := gin.New()

// Add the sloggin middleware to all routes.
// The middleware will log all requests attributes.
router.Use(sloggin.New(logger))
router.Use(gin.Recovery())

// Example pong request.
router.GET("/pong", func(c *gin.Context) {
	// Add an attribute to a single log entry.
	sloggin.AddCustomAttributes(c, slog.String("foo", "bar"))
    c.String(http.StatusOK, "pong")
})

router.Run(":1234")

// output:
// time=2023-10-15T20:32:58.926+02:00 level=INFO msg="Incoming request" environment=production server=gin/1.9.0 gin_mode=release request.time=2023-10-15T20:32:58.626+02:00 request.method=GET request.path=/ request.query="" request.route="" request.ip=127.0.0.1:63932 request.length=0 response.time=2023-10-15T20:32:58.926+02:00 response.latency=100ms response.status=200 response.length=7 id="" foo=bar
```

### JSON output

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

router := gin.New()

// Add the sloggin middleware to all routes.
// The middleware will log all requests attributes.
router.Use(sloggin.New(logger))
router.Use(gin.Recovery())

// Example pong request.
router.GET("/pong", func(c *gin.Context) {
    c.String(http.StatusOK, "pong")
})

router.Run(":1234")

// output:
// {"time":"2023-10-15T20:32:58.926+02:00","level":"INFO","msg":"Incoming request","gin_mode":"GIN_MODE","env":"production","http":{"request":{"time":"2023-10-15T20:32:58.626+02:00","method":"GET","path":"/","query":"","route":"","ip":"127.0.0.1:55296","length":0},"response":{"time":"2023-10-15T20:32:58.926+02:00","latency":100000,"status":200,"length":7},"id":""}}
```

## 🤝 Contributing

- Ping me on twitter [@samuelberthe](https://twitter.com/samuelberthe) (DMs, mentions, whatever :))
- Fork the [project](https://github.com/samber/slog-gin)
- Fix [open issues](https://github.com/samber/slog-gin/issues) or request new features

Don't hesitate ;)

```bash
# Install some dev dependencies
make tools

# Run tests
make test
# or
make watch-test
```

## 👤 Contributors

![Contributors](https://contrib.rocks/image?repo=samber/slog-gin)

## 💫 Show your support

Give a ⭐️ if this project helped you!

[![GitHub Sponsors](https://img.shields.io/github/sponsors/samber?style=for-the-badge)](https://github.com/sponsors/samber)

## 📝 License

Copyright © 2023 [Samuel Berthe](https://github.com/samber).

This project is [MIT](./LICENSE) licensed.
