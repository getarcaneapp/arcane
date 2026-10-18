package health

import (
	"context"
	"net/http"

	"github.com/getarcaneapp/arcane/types/v2/system"
	kit "go.getarcane.app/kit/pkg"
)

type healthHandler struct {
	service *Service
}

type healthOutput struct {
	Status int
	Body   system.HealthResponse
}

type headHealthOutput struct {
	Status int
}

type readyOutput struct {
	Status int
	Body   system.ReadinessResponse
}

type healthOutputs interface {
	healthOutput | headHealthOutput
}

// health reports DOWN once the Francis host has exited; startup stays UP.
func health[T healthOutputs](h *healthHandler) func(context.Context, *struct{}) (*T, error) {
	return func(context.Context, *struct{}) (*T, error) {
		status, body := http.StatusOK, system.HealthResponse{Status: "UP"}
		select {
		case <-h.service.shutdown:
			status, body = http.StatusServiceUnavailable, system.HealthResponse{Status: "DOWN"}
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
