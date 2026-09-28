package transfer

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

// Dependencies are the collaborators the transfer domain needs.
type Dependencies struct {
	DB          *database.DB
	Config      *config.Config
	Docker      *docker.DockerClientService
	KV          *kv.KVService
	Volume      *volume.VolumeService
	Project     *project.ProjectService
	Environment *environment.EnvironmentService
	Activity    *activity.ActivityService
	Role        *role.RoleService
	Upload      *upload.UploadService
	Settings    *settings.SettingsService
}

// Module wires the transfer domain: the per-node routes every environment
// serves and, on managers, the coordinator API.
type Module struct {
	service *Service
	cfg     *config.Config
}

// New builds the transfer domain from its dependencies.
func New(deps Dependencies) *Module {
	return &Module{service: NewService(deps), cfg: deps.Config}
}

// Service exposes the coordinator for bootstrap wiring.
func (m *Module) Service() *Service {
	if m == nil {
		return nil
	}
	return m.service
}

// Start begins staged-export housekeeping for the process lifetime.
func (m *Module) Start(ctx context.Context) { m.service.Start(ctx) }

// SetScheduler injects the durable scheduler on managers and re-registers
// unfinished transfers.
func (m *Module) SetScheduler(ctx context.Context, scheduler schedulertypes.DynamicScheduler, admission *actors.Gate[actors.AdmissionKey]) error {
	return m.service.SetScheduler(ctx, scheduler, admission)
}

// RegisterRoutes mounts the node routes everywhere and the manager API
// outside agent mode. A nil module still registers so OpenAPI generation
// discovers the routes.
func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		registerNodeRoutesInternal(api, nil)
		registerManagerRoutesInternal(api, nil)
		return
	}
	registerNodeRoutesInternal(api, m.service)
	if !m.cfg.AgentMode {
		registerManagerRoutesInternal(api, m.service)
	}
}
