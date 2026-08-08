package s3

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/base"
)

const (
	s3DestinationPathInternal          = "/s3-destinations"
	s3DestinationTagInternal           = "S3 Destinations"
	s3ConnectionTestSuccessInternal    = "S3 connection test succeeded"
	s3RemoteSyncFailureMessageInternal = "Failed to fan out S3 destination sync to remote environments"
)

type s3DestinationHandlerInternal struct {
	service                *S3DestinationService
	syncRemoteDestinations func(context.Context) error
}

type listS3DestinationsInputInternal struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Limit"`
}

type listS3DestinationsOutputInternal struct {
	Body struct {
		Data       []backuptypes.S3Destination `json:"data"`
		Pagination base.PaginationResponse     `json:"pagination"`
	}
}

type listAllS3DestinationsOutputInternal struct {
	Body []backuptypes.S3Destination
}

type s3DestinationIDInputInternal struct {
	ID string `path:"id" doc:"S3 destination ID"`
}

type createS3DestinationInputInternal struct {
	Body backuptypes.CreateS3Destination
}

type updateS3DestinationInputInternal struct {
	ID   string `path:"id" doc:"S3 destination ID"`
	Body backuptypes.UpdateS3Destination
}

type s3DestinationOutputInternal struct {
	Body backuptypes.S3Destination
}

type s3DestinationMessageOutputInternal struct {
	Body base.ApiResponse[base.MessageResponse]
}

type testS3DestinationInputInternal struct {
	ID   string                           `path:"id" doc:"S3 destination ID"`
	Body *backuptypes.UpdateS3Destination `json:"body,omitempty"`
}

type testS3DestinationConfigurationInputInternal struct {
	Body backuptypes.CreateS3Destination
}

type syncS3DestinationsInputInternal struct {
	Body backuptypes.S3DestinationSyncRequest
}

func RegisterS3Destinations(api huma.API, service *S3DestinationService, syncRemoteDestinations func(context.Context) error) {
	handler := &s3DestinationHandlerInternal{service: service, syncRemoteDestinations: syncRemoteDestinations}

	handlerutil.RegisterSecured(api,
		handlerutil.Operation("list-s3-destinations", http.MethodGet, s3DestinationPathInternal, "List S3 destinations", "List saved S3-compatible backup destinations with search, sorting, and pagination", s3DestinationTagInternal),
		authz.PermSettingsRead, handler.listInternal)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("list-all-s3-destinations", http.MethodGet, s3DestinationPathInternal+"/options", "List all S3 destination options", "List saved S3-compatible destinations for backup configuration selectors", s3DestinationTagInternal),
		authz.PermSettingsRead, handler.listAllInternal)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("sync-s3-destinations", http.MethodPost, s3DestinationPathInternal+"/sync", "Sync S3 destinations", "Synchronize manager-owned S3 destinations to an agent", s3DestinationTagInternal),
		authz.PermSettingsWrite, handler.syncInternal)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("get-s3-destination", http.MethodGet, s3DestinationPathInternal+"/{id}", "Get S3 destination", "", s3DestinationTagInternal),
		authz.PermSettingsRead, handler.getInternal)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("create-s3-destination", http.MethodPost, s3DestinationPathInternal, "Create S3 destination", "", s3DestinationTagInternal),
		authz.PermSettingsWrite, handler.createInternal)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("update-s3-destination", http.MethodPut, s3DestinationPathInternal+"/{id}", "Update S3 destination", "", s3DestinationTagInternal),
		authz.PermSettingsWrite, handler.updateInternal)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("test-s3-destination-configuration", http.MethodPost, s3DestinationPathInternal+"/test", "Test unsaved S3 destination configuration", "Verify upload, download, and delete access before saving an S3 destination", s3DestinationTagInternal),
		authz.PermSettingsWrite, handler.testConfigurationInternal)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("test-s3-destination", http.MethodPost, s3DestinationPathInternal+"/{id}/test", "Test S3 destination", "Verify upload, download, and delete access using the saved or supplied S3 destination configuration", s3DestinationTagInternal),
		authz.PermSettingsWrite, handler.testInternal)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("delete-s3-destination", http.MethodDelete, s3DestinationPathInternal+"/{id}", "Delete S3 destination", "", s3DestinationTagInternal),
		authz.PermSettingsWrite, handler.deleteInternal)
}

func (h *s3DestinationHandlerInternal) listInternal(ctx context.Context, input *listS3DestinationsInputInternal) (*listS3DestinationsOutputInternal, error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	destinations, paginationResponse, err := h.service.ListS3Destinations(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	output := &listS3DestinationsOutputInternal{}
	output.Body.Data = destinations
	output.Body.Pagination = handlerutil.PaginationResponse(paginationResponse)
	return output, nil
}

func (h *s3DestinationHandlerInternal) listAllInternal(ctx context.Context, _ *struct{}) (*listAllS3DestinationsOutputInternal, error) {
	destinations, err := h.service.ListAllS3Destinations(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &listAllS3DestinationsOutputInternal{Body: destinations}, nil
}

func (h *s3DestinationHandlerInternal) getInternal(ctx context.Context, input *s3DestinationIDInputInternal) (*s3DestinationOutputInternal, error) {
	destination, err := h.service.GetS3Destination(ctx, input.ID)
	if errors.Is(err, ErrS3DestinationNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &s3DestinationOutputInternal{Body: *destination}, nil
}

func (h *s3DestinationHandlerInternal) createInternal(ctx context.Context, input *createS3DestinationInputInternal) (*s3DestinationOutputInternal, error) {
	if err := h.service.TestS3DestinationConfiguration(ctx, input.Body); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	destination, err := h.service.CreateS3Destination(ctx, input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	h.triggerRemoteSyncInternal(ctx, "S3 destination creation")
	return &s3DestinationOutputInternal{Body: *destination}, nil
}

func (h *s3DestinationHandlerInternal) updateInternal(ctx context.Context, input *updateS3DestinationInputInternal) (*s3DestinationOutputInternal, error) {
	if err := h.service.TestS3Destination(ctx, input.ID, &input.Body); err != nil {
		if errors.Is(err, ErrS3DestinationNotFound) {
			return nil, huma.Error404NotFound(err.Error())
		}
		return nil, huma.Error400BadRequest(err.Error())
	}
	destination, err := h.service.UpdateS3Destination(ctx, input.ID, input.Body)
	if errors.Is(err, ErrS3DestinationNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	h.triggerRemoteSyncInternal(ctx, "S3 destination update")
	return &s3DestinationOutputInternal{Body: *destination}, nil
}

func (h *s3DestinationHandlerInternal) deleteInternal(ctx context.Context, input *s3DestinationIDInputInternal) (*s3DestinationMessageOutputInternal, error) {
	err := h.service.DeleteS3Destination(ctx, input.ID)
	if errors.Is(err, ErrS3DestinationNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if errors.Is(err, ErrS3DestinationInUse) {
		return nil, huma.Error409Conflict(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	h.triggerRemoteSyncInternal(ctx, "S3 destination deletion")
	return &s3DestinationMessageOutputInternal{Body: successMessageResponseInternal("S3 destination deleted successfully")}, nil
}

func (h *s3DestinationHandlerInternal) testInternal(ctx context.Context, input *testS3DestinationInputInternal) (*s3DestinationMessageOutputInternal, error) {
	if err := h.service.TestS3Destination(ctx, input.ID, input.Body); err != nil {
		if errors.Is(err, ErrS3DestinationNotFound) {
			return nil, huma.Error404NotFound(err.Error())
		}
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &s3DestinationMessageOutputInternal{Body: successMessageResponseInternal(s3ConnectionTestSuccessInternal)}, nil
}

func successMessageResponseInternal(message string) base.ApiResponse[base.MessageResponse] {
	return base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: message}}
}

func (h *s3DestinationHandlerInternal) triggerRemoteSyncInternal(ctx context.Context, reason string) {
	if h.syncRemoteDestinations == nil {
		return
	}
	detachedCtx := context.WithoutCancel(ctx)
	go func(syncCtx context.Context, syncReason string) {
		if err := h.syncRemoteDestinations(syncCtx); err != nil {
			slog.WarnContext(syncCtx, s3RemoteSyncFailureMessageInternal, "reason", syncReason, "error", err.Error())
		}
	}(detachedCtx, reason)
}

func (h *s3DestinationHandlerInternal) testConfigurationInternal(ctx context.Context, input *testS3DestinationConfigurationInputInternal) (*s3DestinationMessageOutputInternal, error) {
	if err := h.service.TestS3DestinationConfiguration(ctx, input.Body); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &s3DestinationMessageOutputInternal{Body: successMessageResponseInternal(s3ConnectionTestSuccessInternal)}, nil
}

func (h *s3DestinationHandlerInternal) syncInternal(ctx context.Context, input *syncS3DestinationsInputInternal) (*s3DestinationMessageOutputInternal, error) {
	if err := h.service.SyncS3Destinations(ctx, input.Body.Destinations); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &s3DestinationMessageOutputInternal{Body: successMessageResponseInternal("S3 destinations synced successfully")}, nil
}
