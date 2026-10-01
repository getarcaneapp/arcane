// Package job owns background-job schedule configuration and its HTTP surface.
package job

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
)

type Module struct {
	service     *JobService
	environment *environment.EnvironmentService
}

func New(service *JobService, environment *environment.EnvironmentService) *Module {
	return &Module{service: service, environment: environment}
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
