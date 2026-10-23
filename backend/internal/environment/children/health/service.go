package health

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/edge"
)

// Service runs connectivity probes; the parent persists the resulting status.
type Service struct {
	dockerService *docker.DockerClientService
}

func New(dockerService *docker.DockerClientService) *Service {
	return &Service{dockerService: dockerService}
}

func (s *Service) Probe(ctx context.Context, id string, authenticated func(context.Context) (int, error)) (string, error) {
	if id == "0" {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if _, pingErr := s.dockerService.Call(reqCtx, func(ctx context.Context, cli *client.Client) (client.PingResult, error) {
			return cli.Ping(ctx, client.PingOptions{})
		}); pingErr != nil {
			return "offline", fmt.Errorf("docker ping failed: %w", pingErr)
		}
		return "online", nil
	}
	if edge.GetPollRuntimeRegistry().Get(id, time.Now()).IsPresent() {
		return "online", nil
	}
	if !edge.HasActiveTunnel(id) {
		return "offline", errors.New("edge agent is not connected")
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	statusCode, err := authenticated(reqCtx)
	switch {
	case err != nil:
		return "offline", fmt.Errorf("health check failed: %w", err)
	case statusCode == http.StatusOK:
		return "online", nil
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return "error", fmt.Errorf("environment rejected the access token: status code %d", statusCode)
	default:
		return "error", fmt.Errorf("unexpected status code: %d", statusCode)
	}
}
