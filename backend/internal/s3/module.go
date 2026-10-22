// Package s3 owns reusable S3-compatible backup destinations and their HTTP surface.
package s3

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service                *S3DestinationService
	syncRemoteDestinations func(context.Context)
}

func New(service *S3DestinationService, syncRemoteDestinations func(context.Context)) *Module {
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

func RegisterS3Destinations(api huma.API, service *S3DestinationService, syncRemoteDestinations func(context.Context)) {
	handler := &s3DestinationHandler{service: service, syncRemoteDestinations: syncRemoteDestinations}

	handlerutil.RegisterSecured(api,
		handlerutil.Operation(
			"list-s3-destinations",
			http.MethodGet,
			s3DestinationPath,
			"List S3 destinations",
			"List saved S3-compatible backup destinations with search, sorting, and pagination",
			s3DestinationTag,
		),
		authz.PermS3DestinationsList, handler.list)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation(
			"list-all-s3-destinations",
			http.MethodGet,
			s3DestinationPath+"/options",
			"List all S3 destination options",
			"List saved S3-compatible destinations for backup configuration selectors",
			s3DestinationTag,
		),
		authz.PermS3DestinationsList, handler.listAll)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation(
			"sync-s3-destinations",
			http.MethodPost,
			s3DestinationPath+"/sync",
			"Sync S3 destinations",
			"Synchronize manager-owned S3 destinations to an agent",
			s3DestinationTag,
		),
		authz.PermS3DestinationsSync, handler.sync)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-s3-destination", http.MethodGet, s3DestinationPath+"/{id}", "Get S3 destination", "", s3DestinationTag),
		authz.PermS3DestinationsRead, handler.get)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("create-s3-destination", http.MethodPost, s3DestinationPath, "Create S3 destination", "", s3DestinationTag),
		authz.PermS3DestinationsCreate, handler.create)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("update-s3-destination", http.MethodPatch, s3DestinationPath+"/{id}", "Update S3 destination", "", s3DestinationTag),
		authz.PermS3DestinationsUpdate, handler.update)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation(
			"test-s3-destination-configuration",
			http.MethodPost,
			s3DestinationPath+"/test",
			"Test unsaved S3 destination configuration",
			"Verify upload, download, and delete access before saving an S3 destination",
			s3DestinationTag,
		),
		authz.PermS3DestinationsTest, handler.testConfiguration)
	middleware.RegisterWithActionPermissions(api,
		huma.Operation{
			OperationID: "s3-destination-action",
			Method:      http.MethodPost,
			Path:        s3DestinationPath + "/{id}",
			Summary:     "Test S3 destination",
			Description: "Verify upload, download, and delete access using the saved or supplied S3 destination configuration",
			Tags:        []string{s3DestinationTag},
			Security:    handlerutil.DefaultOperationSecurity(),
		},
		"action",
		map[string]string{
			"test": authz.PermS3DestinationsTest,
		},
		handler.test)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation(
			"get-s3-destination-usage",
			http.MethodGet,
			s3DestinationPath+"/{id}/usage",
			"Check S3 destination references",
			"Report whether backup records, policies, or settings on this environment still reference the destination",
			s3DestinationTag,
		),
		authz.PermS3DestinationsRead, handler.inUse)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("delete-s3-destination", http.MethodDelete, s3DestinationPath+"/{id}", "Delete S3 destination", "", s3DestinationTag),
		authz.PermS3DestinationsDelete, handler.delete)
}
