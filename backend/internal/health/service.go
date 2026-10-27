package health

import (
	"context"
	"fmt"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis"
)

const (
	readyStatus    = "ready"
	notReadyStatus = "not_ready"
	checkOK        = "ok"
	checkTimeout   = 2 * time.Second
)

// Service checks the dependencies the manager needs before it can serve traffic.
type Service struct {
	db       *database.DB
	docker   *docker.DockerClientService
	shutdown <-chan struct{}
}

// NewService builds the readiness checker over the database, the local Docker client, and the actor runtime's shutdown signal.
func NewService(db *database.DB, dockerService *docker.DockerClientService, actors *francis.Runtime) *Service {
	return &Service{
		db:       db,
		docker:   dockerService,
		shutdown: actors.Done(),
	}
}

// Ready pings the database and local Docker daemon; it is never ready while shutting down.
func (s *Service) Ready(ctx context.Context) types.ReadinessResponse {
	select {
	case <-s.shutdown:
		return types.ReadinessResponse{
			Status: notReadyStatus,
			Checks: map[string]string{"shutdown": "in progress"},
		}
	default:
	}

	checks := map[string]string{
		"database": func() string {
			sqlDB, err := s.db.DB.DB()
			if err != nil {
				return fmt.Sprintf("get database handle: %v", err)
			}
			pingCtx, cancel := context.WithTimeout(ctx, checkTimeout)
			defer cancel()
			if pingErr := sqlDB.PingContext(pingCtx); pingErr != nil {
				return pingErr.Error()
			}
			return checkOK
		}(),
		"docker": func() string {
			pingCtx, cancel := context.WithTimeout(ctx, checkTimeout)
			defer cancel()
			dockerClient, err := s.docker.GetClient(pingCtx)
			if err != nil {
				return fmt.Sprintf("connect to Docker: %v", err)
			}
			if _, pingErr := dockerClient.Ping(pingCtx, client.PingOptions{}); pingErr != nil {
				return pingErr.Error()
			}
			return checkOK
		}(),
	}
	for _, result := range checks {
		if result != checkOK {
			return types.ReadinessResponse{Status: notReadyStatus, Checks: checks}
		}
	}
	return types.ReadinessResponse{Status: readyStatus, Checks: checks}
}
