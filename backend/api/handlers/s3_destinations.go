package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/base"
)

type S3DestinationHandler struct {
	service            *services.S3DestinationService
	environmentService *services.EnvironmentService
}

type ListS3DestinationsInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Limit"`
}

type ListS3DestinationsOutput struct {
	Body struct {
		Data       []backuptypes.S3Destination `json:"data"`
		Pagination base.PaginationResponse     `json:"pagination"`
	}
}

type ListAllS3DestinationsOutput struct {
	Body []backuptypes.S3Destination
}

type S3DestinationInput struct {
	ID string `path:"id" doc:"S3 destination ID"`
}

type CreateS3DestinationInput struct {
	Body backuptypes.CreateS3Destination
}

type UpdateS3DestinationInput struct {
	ID   string `path:"id" doc:"S3 destination ID"`
	Body backuptypes.UpdateS3Destination
}

type S3DestinationOutput struct {
	Body backuptypes.S3Destination
}

type DeleteS3DestinationOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type SyncS3DestinationsInput struct {
	Body backuptypes.S3DestinationSyncRequest
}

type SyncS3DestinationsOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

func RegisterS3Destinations(api huma.API, service *services.S3DestinationService, environmentService *services.EnvironmentService) {
	handler := &S3DestinationHandler{service: service, environmentService: environmentService}

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-s3-destinations",
		Method:      http.MethodGet,
		Path:        "/s3-destinations",
		Summary:     "List S3 destinations",
		Description: "List saved S3-compatible backup destinations with search, sorting, and pagination",
		Tags:        []string{"S3 Destinations"},
	}, authz.PermSettingsRead, handler.List)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-all-s3-destinations",
		Method:      http.MethodGet,
		Path:        "/s3-destinations/options",
		Summary:     "List all S3 destination options",
		Description: "List saved S3-compatible destinations for backup configuration selectors",
		Tags:        []string{"S3 Destinations"},
	}, authz.PermSettingsRead, handler.ListAll)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "sync-s3-destinations",
		Method:      http.MethodPost,
		Path:        "/s3-destinations/sync",
		Summary:     "Sync S3 destinations",
		Description: "Synchronize manager-owned S3 destinations to an agent",
		Tags:        []string{"S3 Destinations"},
	}, authz.PermSettingsWrite, handler.Sync)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-s3-destination",
		Method:      http.MethodGet,
		Path:        "/s3-destinations/{id}",
		Summary:     "Get S3 destination",
		Tags:        []string{"S3 Destinations"},
	}, authz.PermSettingsRead, handler.Get)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-s3-destination",
		Method:      http.MethodPost,
		Path:        "/s3-destinations",
		Summary:     "Create S3 destination",
		Tags:        []string{"S3 Destinations"},
	}, authz.PermSettingsWrite, handler.Create)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-s3-destination",
		Method:      http.MethodPut,
		Path:        "/s3-destinations/{id}",
		Summary:     "Update S3 destination",
		Tags:        []string{"S3 Destinations"},
	}, authz.PermSettingsWrite, handler.Update)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-s3-destination",
		Method:      http.MethodDelete,
		Path:        "/s3-destinations/{id}",
		Summary:     "Delete S3 destination",
		Tags:        []string{"S3 Destinations"},
	}, authz.PermSettingsWrite, handler.Delete)
}

func (h *S3DestinationHandler) List(ctx context.Context, input *ListS3DestinationsInput) (*ListS3DestinationsOutput, error) {
	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	destinations, paginationResponse, err := h.service.ListS3Destinations(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	output := &ListS3DestinationsOutput{}
	output.Body.Data = destinations
	output.Body.Pagination = toPaginationResponseInternal(paginationResponse)
	return output, nil
}

func (h *S3DestinationHandler) ListAll(ctx context.Context, _ *struct{}) (*ListAllS3DestinationsOutput, error) {
	destinations, err := h.service.ListAllS3Destinations(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &ListAllS3DestinationsOutput{Body: destinations}, nil
}

func (h *S3DestinationHandler) Get(ctx context.Context, input *S3DestinationInput) (*S3DestinationOutput, error) {
	destination, err := h.service.GetS3Destination(ctx, input.ID)
	if errors.Is(err, services.ErrS3DestinationNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &S3DestinationOutput{Body: *destination}, nil
}

func (h *S3DestinationHandler) Create(ctx context.Context, input *CreateS3DestinationInput) (*S3DestinationOutput, error) {
	destination, err := h.service.CreateS3Destination(ctx, input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	h.triggerRemoteSyncInternal(ctx, "S3 destination creation")
	return &S3DestinationOutput{Body: *destination}, nil
}

func (h *S3DestinationHandler) Update(ctx context.Context, input *UpdateS3DestinationInput) (*S3DestinationOutput, error) {
	destination, err := h.service.UpdateS3Destination(ctx, input.ID, input.Body)
	if errors.Is(err, services.ErrS3DestinationNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	h.triggerRemoteSyncInternal(ctx, "S3 destination update")
	return &S3DestinationOutput{Body: *destination}, nil
}

func (h *S3DestinationHandler) Delete(ctx context.Context, input *S3DestinationInput) (*DeleteS3DestinationOutput, error) {
	err := h.service.DeleteS3Destination(ctx, input.ID)
	if errors.Is(err, services.ErrS3DestinationNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if errors.Is(err, services.ErrS3DestinationInUse) {
		return nil, huma.Error409Conflict(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	h.triggerRemoteSyncInternal(ctx, "S3 destination deletion")
	return &DeleteS3DestinationOutput{Body: base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "S3 destination deleted successfully"},
	}}, nil
}

func (h *S3DestinationHandler) Sync(ctx context.Context, input *SyncS3DestinationsInput) (*SyncS3DestinationsOutput, error) {
	if err := h.service.SyncS3Destinations(ctx, input.Body.Destinations); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &SyncS3DestinationsOutput{Body: base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "S3 destinations synced successfully"},
	}}, nil
}

func (h *S3DestinationHandler) triggerRemoteSyncInternal(ctx context.Context, reason string) {
	if h.environmentService == nil {
		return
	}
	detachedCtx := context.WithoutCancel(ctx)
	go func(syncCtx context.Context) {
		if err := h.environmentService.SyncS3DestinationsToRemoteEnvironments(syncCtx); err != nil {
			slog.WarnContext(syncCtx, "Failed to fan out S3 destination sync to remote environments", "reason", reason, "error", err)
		}
	}(detachedCtx)
}
