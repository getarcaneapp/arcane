package logging

import (
	"context"
	"log/slog"

	"emperror.dev/emperror"
	"emperror.dev/errors"
)

var _ emperror.ErrorHandler = (*SlogErrorHandler)(nil)
var _ emperror.ErrorHandlerContext = (*SlogErrorHandler)(nil)

// SlogErrorHandler reports handled errors through the process slog logger.
type SlogErrorHandler struct{}

func (SlogErrorHandler) Handle(err error) {
	SlogErrorHandler{}.HandleContext(context.Background(), err)
}

func (SlogErrorHandler) HandleContext(ctx context.Context, err error) {
	if err == nil {
		return
	}

	attrs := []any{"error", err}
	details := errors.GetDetails(err)
	for i := 0; i+1 < len(details); i += 2 {
		attrs = append(attrs, details[i], details[i+1])
	}
	slog.ErrorContext(ctx, "Unhandled error", attrs...)
}

// NewSlogErrorHandler creates the application error handler.
func NewSlogErrorHandler() *SlogErrorHandler {
	return &SlogErrorHandler{}
}
