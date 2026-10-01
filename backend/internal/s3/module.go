// Package s3 owns reusable S3-compatible backup destinations and their HTTP surface.
package s3

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

type Module struct {
	service                *S3DestinationService
	syncRemoteDestinations func(context.Context) error
}

func New(service *S3DestinationService, syncRemoteDestinations func(context.Context) error) *Module {
	return &Module{service: service, syncRemoteDestinations: syncRemoteDestinations}
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
	RegisterS3Destinations(api, m.service, m.syncRemoteDestinations)
}
