// Package apns owns mobile push device pairing, the relay outbox, and its HTTP routes.
package apns

import "github.com/danielgtaylor/huma/v2"

type Module struct {
	service *ApnsService
}

func New(service *ApnsService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *ApnsService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterApns(api, nil)
		return
	}
	RegisterApns(api, m.service)
}
