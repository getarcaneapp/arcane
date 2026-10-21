// Package health probes environment connectivity over direct HTTP, edge
// tunnels or the local Docker socket.
package health

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

// Service runs connectivity probes; the parent persists the resulting status.
type Service struct {
	httpClient    *http.Client
	dockerService *docker.DockerClientService
}

func New(httpClient *http.Client, dockerService *docker.DockerClientService) *Service {
	return &Service{httpClient: httpClient, dockerService: dockerService}
}

// Probe reports "online", "offline" or "error" for one environment. A custom
// API URL always probes directly over HTTP.
func (s *Service) Probe(ctx context.Context, id, apiURL string, isEdge, customURL bool, authenticated func(context.Context) (int, error)) (string, error) {
	if id == "0" && !customURL {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if _, pingErr := s.dockerService.Call(reqCtx, func(ctx context.Context, cli *client.Client) (client.PingResult, error) {
			return cli.Ping(ctx, client.PingOptions{})
		}); pingErr != nil {
			return "offline", fmt.Errorf("docker ping failed: %w", pingErr)
		}
		return "online", nil
	}
	if isEdge && !customURL {
		if edge.GetPollRuntimeRegistry().Get(id, time.Now()).IsPresent() {
			return "online", nil
		}
		if !edge.HasActiveTunnel(id) {
			return "offline", errors.New("edge agent is not connected")
		}
	}
	if !customURL {
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

	baseURL, err := httpx.NormalizeBaseURL(apiURL)
	if err != nil {
		return "offline", fmt.Errorf("invalid environment API URL: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/health", http.NoBody)
	if err != nil {
		return "offline", fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "offline", fmt.Errorf("connection failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		return "online", nil
	}
	return "error", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}
