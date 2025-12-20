package utils

import (
	"log/slog"
	"regexp"

	"github.com/gin-gonic/gin"
	sloggin "github.com/samber/slog-gin"
)

func GetLoggerMiddleware() gin.HandlerFunc {
	return sloggin.NewWithFilters(
		slog.Default(),
		sloggin.IgnorePathPrefix("/_app/"),
		sloggin.IgnorePath("/img"),
		sloggin.IgnorePathPrefix("/api/fonts/"),
		sloggin.IgnorePath("/api/health"),
		sloggin.IgnorePathMatch(*regexp.MustCompile(`^/api/environments/[^/]+/ws/`)),
	)
}
