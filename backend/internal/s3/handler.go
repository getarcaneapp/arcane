package s3

import (
	"context"
	"errors"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/getarcaneapp/arcane/types/v2/base"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

const (
	s3DestinationPathInternal          = "/backups/s3"
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

type listAllS3DestinationsOutputInternal struct {
	Body []backup.S3Destination
}

type s3DestinationIDInputInternal struct {
	ID string `path:"id" doc:"S3 destination ID"`
}

type createS3DestinationInputInternal struct {
	Body backup.CreateS3Destination
}

type updateS3DestinationInputInternal struct {
	ID   string `path:"id" doc:"S3 destination ID"`
	Body backup.UpdateS3Destination
}

type s3DestinationOutputInternal struct {
	Body backup.S3Destination
}

type testS3DestinationInputInternal struct {
	ID   string                      `path:"id" doc:"S3 destination ID"`
	Body *backup.UpdateS3Destination `json:"body,omitempty"`
}

type testS3DestinationConfigurationInputInternal struct {
	Body backup.CreateS3Destination
}

type syncS3DestinationsInputInternal struct {
	Body backup.S3DestinationSyncRequest
}

type s3DestinationUsageOutputInternal struct {
	Body struct {
		InUse bool `json:"inUse"`
	}
}

func (h *s3DestinationHandlerInternal) inUseInternal(ctx context.Context, input *s3DestinationIDInputInternal) (*s3DestinationUsageOutputInternal, error) {
	inUse, err := h.service.DestinationInUse(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	output := &s3DestinationUsageOutputInternal{}
	output.Body.InUse = inUse
	return output, nil
}

func (h *s3DestinationHandlerInternal) listInternal(ctx context.Context, input *listS3DestinationsInputInternal) (*handlerutil.Page[backup.S3Destination], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	destinations, paginationResponse, err := h.service.ListS3Destinations(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Page[backup.S3Destination]{
		Body: base.Paginated[backup.S3Destination]{
			Success:    true,
			Data:       destinations,
			Pagination: handlerutil.PaginationResponse(paginationResponse),
		},
	}, nil
}

func (h *s3DestinationHandlerInternal) listAllInternal(ctx context.Context, _ *struct{}) (*listAllS3DestinationsOutputInternal, error) {
	destinations, err := h.service.ListAllS3Destinations(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &listAllS3DestinationsOutputInternal{Body: destinations}, nil
}

func (h *s3DestinationHandlerInternal) getInternal(ctx context.Context, input *s3DestinationIDInputInternal) (*handlerutil.Out[backup.S3Destination], error) {
	destination, err := h.service.GetS3Destination(ctx, input.ID)
	if errors.Is(err, ErrS3DestinationNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[backup.S3Destination]{Body: base.ApiResponse[backup.S3Destination]{Success: true, Data: *destination}}, nil
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

func (h *s3DestinationHandlerInternal) deleteInternal(ctx context.Context, input *s3DestinationIDInputInternal) (*handlerutil.Out[base.MessageResponse], error) {
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
	return handlerutil.MessageOutput("S3 destination deleted successfully", ""), nil
}

func (h *s3DestinationHandlerInternal) testInternal(ctx context.Context, input *testS3DestinationInputInternal) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.service.TestS3Destination(ctx, input.ID, input.Body); err != nil {
		if errors.Is(err, ErrS3DestinationNotFound) {
			return nil, huma.Error404NotFound(err.Error())
		}
		return nil, huma.Error400BadRequest(err.Error())
	}
	return handlerutil.MessageOutput(s3ConnectionTestSuccessInternal, ""), nil
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

func (h *s3DestinationHandlerInternal) testConfigurationInternal(ctx context.Context, input *testS3DestinationConfigurationInputInternal) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.service.TestS3DestinationConfiguration(ctx, input.Body); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return handlerutil.MessageOutput(s3ConnectionTestSuccessInternal, ""), nil
}

func (h *s3DestinationHandlerInternal) syncInternal(ctx context.Context, input *syncS3DestinationsInputInternal) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.service.SyncS3Destinations(ctx, input.Body.Destinations); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return handlerutil.MessageOutput("S3 destinations synced successfully", ""), nil
}
