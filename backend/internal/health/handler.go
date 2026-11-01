package health

import (
	"context"
	"net/http"

	"github.com/getarcaneapp/arcane/types/v3"
	kit "kit.getarcane.app"
)

type (
	healthHandler struct {
		service *Service
	}

	healthOutput struct {
		Body   types.HealthResponse
		Status int
	}

	headHealthOutput struct {
		Status int
	}

	readyOutput struct {
		Body   types.ReadinessResponse
		Status int
	}

	healthOutputs interface {
		healthOutput | headHealthOutput
	}
)

// health reports DOWN once the Francis host has exited; startup stays UP.
func health[T healthOutputs](h *healthHandler) func(context.Context, *struct{}) (*T, error) {
	return func(context.Context, *struct{}) (*T, error) {
		status, body := http.StatusOK, types.HealthResponse{Status: "UP"}
		select {
		case <-h.service.shutdown:
			status, body = http.StatusServiceUnavailable, types.HealthResponse{Status: "DOWN"}
		default:
		}
		var output T
		switch typed := any(&output).(type) {
		case *healthOutput:
			*typed = healthOutput{Status: status, Body: body}
		case *headHealthOutput:
			*typed = headHealthOutput{Status: status}
		}
		return &output, nil
	}
}

// ready serves the readiness probe; 503 lists the failing check.
func (h *healthHandler) ready(ctx context.Context, _ *struct{}) (*readyOutput, error) {
	result := h.service.Ready(ctx)
	return &readyOutput{
		Status: kit.Ternary(result.Status == readyStatus, http.StatusOK, http.StatusServiceUnavailable),
		Body:   result,
	}, nil
}
