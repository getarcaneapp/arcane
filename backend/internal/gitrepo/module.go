// Package gitrepo owns configured Git repositories and their HTTP surface.
package gitrepo

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

type Dependencies struct {
	DB       *database.DB
	WorkDir  string
	Event    *event.EventService
	Settings *settings.SettingsService
}

type Module struct {
	service *GitRepositoryService
}

func New(deps Dependencies) *Module {
	return &Module{service: NewGitRepositoryService(deps.DB, deps.WorkDir, deps.Event, deps.Settings)}
}

func (m *Module) Service() *GitRepositoryService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterGitRepositories(api, nil)
		return
	}
	RegisterGitRepositories(api, m.service)
}
