package bootstrap

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
	slogGorm "github.com/orandin/slog-gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"gorm.io/gorm/logger"
)

func SetupGinLogger(cfg *config.Config) {
	var lvl slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	lv := new(slog.LevelVar)
	lv.Set(lvl)

	var h slog.Handler
	if cfg.LogJson {
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv})
	} else {
		h = tint.NewHandler(os.Stdout, &tint.Options{
			Level:      lv,
			TimeFormat: "Jan 02 15:04:05.000",
		})
	}

	slog.SetDefault(slog.New(h))
}

func BuildGormLogger(cfg *config.Config) logger.Interface {
	lvl := strings.ToLower(cfg.LogLevel)

	opts := []slogGorm.Option{
		slogGorm.WithHandler(slog.Default().Handler()),
		slogGorm.WithSlowThreshold(200 * time.Millisecond),
	}

	var defaultTypeLevel slog.Level
	switch lvl {
	case "debug":
		defaultTypeLevel = slog.LevelDebug
		// Trace all SQL messages only in debug
		opts = append(opts, slogGorm.WithTraceAll())
	case "warn", "warning":
		defaultTypeLevel = slog.LevelWarn
	case "error":
		defaultTypeLevel = slog.LevelError
	default:
		defaultTypeLevel = slog.LevelInfo
	}

	opts = append(opts,
		slogGorm.SetLogLevel(slogGorm.DefaultLogType, defaultTypeLevel),
		slogGorm.SetLogLevel(slogGorm.ErrorLogType, slog.LevelError),
		slogGorm.SetLogLevel(slogGorm.SlowQueryLogType, slog.LevelWarn),
	)

	return slogGorm.New(opts...)
}

func ConfigureGormLogger(cfg *config.Config) {
	database.SetGormLogger(BuildGormLogger(cfg))
}
