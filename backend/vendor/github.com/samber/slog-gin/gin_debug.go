package sloggin

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"
)

// SetDebugPrintRouteFunc sets the debug print route function for the gin engine.
// If no custom function is provided, the default function will be used.
// The default function logs the route registered with the method, path, handler, and number of handlers.
func SetDebugPrintRouteFunc(logger *slog.Logger, customFunc ...func(httpMethod string, absolutePath string, handlerName string, nuHandlers int)) {
	if len(customFunc) == 0 {
		gin.DebugPrintRouteFunc = func(httpMethod string, absolutePath string, handlerName string, nuHandlers int) {
			logger.Debug("Route registered",
				slog.String("method", httpMethod),
				slog.String("path", absolutePath),
				slog.String("handler", handlerName),
				slog.Int("num_handlers", nuHandlers))

		}
	} else {
		gin.DebugPrintRouteFunc = customFunc[0]
	}
}

// SetDebugPrintFunc sets the debug print function for the gin engine.
// If no custom function is provided, the default function will be used.
// The default function logs the debug message with the format and values.
func SetDebugPrintFunc(logger *slog.Logger, customFunc ...func(format string, values ...any)) {
	if len(customFunc) == 0 {
		gin.DebugPrintFunc = func(format string, values ...any) {
			format = strings.TrimRight(format, "\n")
			logger.Debug(fmt.Sprintf("[GIN-debug] "+format, values...))
		}
	} else {
		gin.DebugPrintFunc = customFunc[0]
	}
}
