package health

import (
	"context"
	"net/http"

	"github.com/getarcaneapp/arcane/types/v2/system"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
)

type healthHandler struct {
	actors *francis.Runtime
}

type healthOutput struct {
	Status int
	Body   system.HealthResponse
}

type headHealthOutput struct {
	Status int
}

type healthOutputs interface {
	healthOutput | headHealthOutput
}

// health reports DOWN once the Francis host has exited; startup stays UP.
func health[T healthOutputs](h *healthHandler) func(context.Context, *struct{}) (*T, error) {
	return func(context.Context, *struct{}) (*T, error) {
		status, body := http.StatusOK, system.HealthResponse{Status: "UP"}
		if h.actors != nil {
			select {
			case <-h.actors.Done():
				status, body = http.StatusServiceUnavailable, system.HealthResponse{Status: "DOWN"}
			default:
			}
		}
		var output T
		switch typed := any(&output).(type) {
		case *healthOutput:
			typed.Status, typed.Body = status, body
		case *headHealthOutput:
			typed.Status = status
		}
		return &output, nil
	}
}
