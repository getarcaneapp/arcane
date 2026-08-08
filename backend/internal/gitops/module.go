// Package gitops owns GitOps synchronization, scheduling, persistence, and routes.
package gitops

import "github.com/danielgtaylor/huma/v2"

type Module struct {
	service *GitOpsSyncService
}

func New(service *GitOpsSyncService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *GitOpsSyncService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterGitOpsSyncs(api, nil)
		return
	}
	RegisterGitOpsSyncs(api, m.service)
}
