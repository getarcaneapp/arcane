package s3

import (
	"context"
	"errors"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

const (
	s3DestinationPath       = "/backups/s3"
	s3DestinationTag        = "S3 Destinations"
	s3ConnectionTestSuccess = "S3 connection test succeeded"
)

type s3DestinationHandler struct {
	service                *S3DestinationService
	syncRemoteDestinations func(context.Context)
}

type listS3DestinationsInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Limit"`
}

type listAllS3DestinationsOutput struct {
	Body []types.S3Destination
}

type s3DestinationIDInput struct {
	ID string `path:"id" doc:"S3 destination ID"`
}

type createS3DestinationInput struct {
	types.Create[types.S3Destination]
}

type updateS3DestinationInput struct {
	ID string `path:"id" doc:"S3 destination ID"`
	types.Update[types.S3Destination]
}

type s3DestinationOutput struct {
	Body types.S3Destination
}

type testS3DestinationInput struct {
	ID   string               `path:"id" doc:"S3 destination ID"`
	Body *types.S3Destination `json:"body,omitempty"`
}

type testS3DestinationConfigurationInput struct {
	Body types.S3Destination
}

type syncS3DestinationsInput struct {
	Body types.S3DestinationSyncRequest
}

type s3DestinationUsageOutput struct {
	Body types.S3DestinationUsage
}

func (h *s3DestinationHandler) inUse(ctx context.Context, input *s3DestinationIDInput) (*s3DestinationUsageOutput, error) {
	inUse, err := h.service.DestinationInUse(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &s3DestinationUsageOutput{Body: types.S3DestinationUsage{InUse: inUse}}, nil
}

func (h *s3DestinationHandler) list(ctx context.Context, input *listS3DestinationsInput) (*handlerutil.Page[types.S3Destination], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	destinations, paginationResponse, err := h.service.ListS3Destinations(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Page[types.S3Destination]{
		Body: types.Paginated[types.S3Destination]{
			Success:    true,
			Data:       destinations,
			Pagination: handlerutil.PaginationResponse(paginationResponse),
		},
	}, nil
}

func (h *s3DestinationHandler) listAll(ctx context.Context, _ *struct{}) (*listAllS3DestinationsOutput, error) {
	destinations, err := h.service.ListAllS3Destinations(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &listAllS3DestinationsOutput{Body: destinations}, nil
}

func (h *s3DestinationHandler) get(ctx context.Context, input *s3DestinationIDInput) (*handlerutil.Out[types.S3Destination], error) {
	destination, err := h.service.GetS3Destination(ctx, input.ID)
	if errors.Is(err, ErrS3DestinationNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[types.S3Destination]{Body: types.ApiResponse[types.S3Destination]{Success: true, Data: *destination}}, nil
}

func (h *s3DestinationHandler) create(ctx context.Context, input *createS3DestinationInput) (*s3DestinationOutput, error) {
	if err := h.service.TestS3DestinationConfiguration(ctx, input.Body); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	destination, err := h.service.CreateS3Destination(ctx, input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	h.triggerRemoteSync(ctx, "S3 destination creation")
	return &s3DestinationOutput{Body: *destination}, nil
}

func (h *s3DestinationHandler) update(ctx context.Context, input *updateS3DestinationInput) (*s3DestinationOutput, error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid S3 destination update body")
	}
	merged, err := h.service.MergeS3Destination(ctx, input.ID, input.Body, submitted)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	if testErr := h.service.TestS3Destination(ctx, input.ID, &merged); testErr != nil {
		return nil, huma.Error400BadRequest(testErr.Error())
	}
	destination, err := h.service.UpdateS3Destination(ctx, input.ID, merged)
	if errors.Is(err, ErrS3DestinationNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	h.triggerRemoteSync(ctx, "S3 destination update")
	return &s3DestinationOutput{Body: *destination}, nil
}

func (h *s3DestinationHandler) delete(ctx context.Context, input *s3DestinationIDInput) (*handlerutil.Out[types.MessageResponse], error) {
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
	h.triggerRemoteSync(ctx, "S3 destination deletion")
	return handlerutil.MessageOutput("S3 destination deleted successfully", ""), nil
}

func (h *s3DestinationHandler) test(ctx context.Context, input *testS3DestinationInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.service.TestS3Destination(ctx, input.ID, input.Body); err != nil {
		if errors.Is(err, ErrS3DestinationNotFound) {
			return nil, huma.Error404NotFound(err.Error())
		}
		return nil, huma.Error400BadRequest(err.Error())
	}
	return handlerutil.MessageOutput(s3ConnectionTestSuccess, ""), nil
}

func (h *s3DestinationHandler) triggerRemoteSync(ctx context.Context, reason string) {
	slog.DebugContext(ctx, "Queueing S3 destination sync to remote environments", "reason", reason)
	h.syncRemoteDestinations(ctx)
}

func (h *s3DestinationHandler) testConfiguration(ctx context.Context, input *testS3DestinationConfigurationInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.service.TestS3DestinationConfiguration(ctx, input.Body); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return handlerutil.MessageOutput(s3ConnectionTestSuccess, ""), nil
}

func (h *s3DestinationHandler) sync(ctx context.Context, input *syncS3DestinationsInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.service.SyncS3Destinations(ctx, input.Body.Destinations); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return handlerutil.MessageOutput("S3 destinations synced successfully", ""), nil
}
