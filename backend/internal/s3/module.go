// Package s3 owns reusable S3-compatible backup destinations and their HTTP surface.
package s3

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

type Dependencies struct {
	DB                     *database.DB
	SyncRemoteDestinations func(context.Context) error
	CheckRemoteReferences  func(ctx context.Context, destinationID string) error
}

type Module struct {
	service *S3DestinationService
	deps    Dependencies
}

func New(deps Dependencies) *Module {
	service := NewS3DestinationService(deps.DB)
	service.checkRemoteReferences = deps.CheckRemoteReferences
	return &Module{service: service, deps: deps}
}

func (m *Module) Service() *S3DestinationService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterS3Destinations(api, nil, nil)
		return
	}
	RegisterS3Destinations(api, m.service, m.deps.SyncRemoteDestinations)
}
