// Package job owns background-job schedule configuration and its HTTP surface.
package job

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

type Dependencies struct {
	DB          *database.DB
	Settings    *settings.SettingsService
	Config      *config.Config
	Environment *environment.EnvironmentService
}

type Module struct {
	service     *JobService
	environment *environment.EnvironmentService
}

func New(deps Dependencies) *Module {
	return &Module{
		service:     NewJobService(deps.DB, deps.Settings, deps.Config),
		environment: deps.Environment,
	}
}

func (m *Module) Service() *JobService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterJobSchedules(api, nil, nil)
		return
	}
	RegisterJobSchedules(api, m.service, m.environment)
}
