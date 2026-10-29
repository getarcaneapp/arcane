package releasenotes

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

const statusClientClosedRequest = 499

type Handler struct {
	service *Service
}

type GetReleaseNotesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageRef      string `query:"imageRef" required:"true" doc:"Image reference"`
	Version       string `query:"version" doc:"Target version or tag"`
	Digest        string `query:"digest" doc:"Target manifest digest"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetReleaseNotes(ctx context.Context, input *GetReleaseNotesInput) (*handlerutil.Out[types.ImageReleaseNotes], error) {
	out, err := h.service.ReleaseNotes(ctx, Request{
		ImageRef: input.ImageRef,
		Version:  input.Version,
		Digest:   input.Digest,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, huma.NewError(statusClientClosedRequest, "request canceled")
		}
		if apiErr := common.ToAPIError(err); apiErr.HTTPStatus() != http.StatusInternalServerError {
			return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
		}
		slog.ErrorContext(ctx, "failed to get image release notes", "image", input.ImageRef, "error", err)
		return nil, huma.Error500InternalServerError("failed to get image release notes")
	}

	return &handlerutil.Out[types.ImageReleaseNotes]{
		Body: types.ApiResponse[types.ImageReleaseNotes]{
			Success: true,
			Data:    *out,
		},
	}, nil
}
