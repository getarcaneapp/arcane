// Package gitrepo owns configured Git repositories and their HTTP surface.
package gitrepo

import (
	"github.com/danielgtaylor/huma/v2"
)

type Module struct {
	service *GitRepositoryService
}

func New(service *GitRepositoryService) *Module {
	return &Module{service: service}
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
