package health

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/pkg/agent"
)

type (
	Service struct {
		dockerService *docker.DockerClientService
		tunnels       *agent.TunnelRegistry
	}
)

func New(dockerService *docker.DockerClientService, tunnels *agent.TunnelRegistry) *Service {
	return &Service{
		dockerService: dockerService,
		tunnels:       tunnels,
	}
}

// Probe reports an environment's status: the manager's own daemon and docker-host environments are
// reached over the Docker API, everything else over its agent tunnel.
func (s *Service) Probe(ctx context.Context, id, dockerHost string, authenticated func(context.Context) (int, error)) (string, error) {
	if id == "0" || strings.TrimSpace(dockerHost) != "" {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		pingErr := s.dockerService.PingHost(reqCtx, dockerHost)
		if pingErr != nil {
			return "offline", fmt.Errorf("docker ping failed: %w", pingErr)
		}
		return "online", nil
	}
	if !s.tunnels.Active(id).IsPresent() {
		return "offline", errors.New("agent is not connected")
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
